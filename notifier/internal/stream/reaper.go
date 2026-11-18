// Reaper — безопасная очистка стрима listings.new (ТЗ 4.3 «Хранение»):
// «Очистка удаляет только уже подтверждённые события за безопасной границей,
// учитывая pending и ещё не прочитанные записи». До границы записи не режутся
// (MAXLEN в XADD и произвольная обрезка запрещены тем же пунктом ТЗ).
package stream

import (
	"context"
	"fmt"
	"log"
	"time"
)

// cleanupInterval — период очистки по умолчанию; переопределяется
// NOTIFIER_STREAM_CLEANUP_INTERVAL (ТЗ 3.6: параметры настраиваются).
const cleanupInterval = 5 * time.Minute

// Reaper периодически удаляет из стрима подтверждённые записи.
// Безопасная граница: запись подтверждена, если она доставлена consumer
// группе (не осталась непрочитанной) и её НЕТ в pending — все
// неподтверждённые записи, включая «в работе», лежат в PEL группы. Поэтому
// граница = минимальный ID в pending: записи строго раньше неё были доставлены
// и вышли из pending (XACK), т.е. подтверждены. Непрочитанные записи стоят
// после доставленных в хвосте стрима и границу не пересекают. XDEL удаляет
// запись из стрима, но не из PEL живых consumer'ов — для подтверждённой
// записи это не важно, её там уже нет.
type Reaper struct {
	rdb      Redis
	interval time.Duration
}

// NewReaper возвращает очиститель с заданным периодом обхода.
func NewReaper(rdb Redis, interval time.Duration) *Reaper {
	return &Reaper{rdb: rdb, interval: interval}
}

// DefaultCleanupInterval — период очистки по умолчанию (ТЗ 3.6: дефолты
// по ТЗ; env переопределяется в main с валидацией).
func DefaultCleanupInterval() time.Duration { return cleanupInterval }

// Run крутит цикл очистки до отмены контекста. Ошибки прохода не фатальны:
// сбой очистки оставляет стрим без обрезки, что безопаснее лишнего удаления.
func (r *Reaper) Run(ctx context.Context) {
	t := time.NewTicker(r.interval)
	defer t.Stop()
	log.Printf("stream reaper: cleanup every %s", r.interval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n, boundary, err := r.CleanOnce(ctx)
			if err != nil {
				log.Printf("stream reaper: %v", err)
				continue
			}
			if n > 0 {
				log.Printf("stream reaper: deleted %d confirmed records (boundary %s)", n, boundary)
			}
		}
	}
}

// CleanOnce выполняет один проход очистки: считает границу, собирает
// подтверждённые записи до неё и удаляет их. Возвращает число удалённых
// записей и границу ("" — чистить нечего).
func (r *Reaper) CleanOnce(ctx context.Context) (int, string, error) {
	boundary, err := r.boundary(ctx)
	if err != nil {
		return 0, "", err
	}
	if boundary == "" {
		return 0, "", nil
	}

	// Записи строго раньше границы (exclusive-диапазон "(") — доставлены
	// группе и подтверждены; сама запись границы не удаляется.
	msgs, err := r.rdb.XRange(ctx, StreamName, "-", "("+boundary).Result()
	if err != nil {
		return 0, boundary, fmt.Errorf("xrange: %w", err)
	}
	if len(msgs) == 0 {
		return 0, boundary, nil
	}
	ids := make([]string, 0, len(msgs))
	for _, m := range msgs {
		ids = append(ids, m.ID)
	}
	if err := r.rdb.XDel(ctx, StreamName, ids...).Err(); err != nil {
		return 0, boundary, fmt.Errorf("xdel: %w", err)
	}
	return len(ids), boundary, nil
}

// boundary вычисляет безопасную границу очистки ("" — чистить нечего).
func (r *Reaper) boundary(ctx context.Context) (string, error) {
	p, err := r.rdb.XPending(ctx, StreamName, GroupName).Result()
	if err != nil {
		return "", fmt.Errorf("xpending: %w", err)
	}
	if p.Count > 0 {
		// Минимальный ID pending (summary-форма XPENDING отдаёт его в Lower):
		// записи строго раньше подтверждены, сама запись в работе не трогается.
		return p.Lower, nil
	}
	// Pending пуст: всё доставленное подтверждено. Граница — last-delivered
	// группы: записи после неё могут быть ещё непрочитаны. Нулевой
	// last-delivered — группа ещё не доставила ничего, чистить нечего.
	groups, err := r.rdb.XInfoGroups(ctx, StreamName).Result()
	if err != nil {
		return "", fmt.Errorf("xinfo groups: %w", err)
	}
	var lastDelivered string
	for _, g := range groups {
		if g.Name == GroupName && g.LastDeliveredID != "" && g.LastDeliveredID != "0-0" {
			lastDelivered = g.LastDeliveredID
			break
		}
	}
	if lastDelivered == "" {
		return "", nil
	}
	// Гонка с диспетчером: между XPENDING выше и XInfoGroups он мог прочитать
	// новую пачку — last-delivered уехал вперёд записей, которые уже лежат в
	// pending. Перепроверяем pending: появился — границей служит его
	// минимальный ID (те же соображения, что и в первой ветке выше).
	p2, err := r.rdb.XPending(ctx, StreamName, GroupName).Result()
	if err != nil {
		return "", fmt.Errorf("xpending recheck: %w", err)
	}
	if p2.Count > 0 && p2.Lower != "" {
		return p2.Lower, nil
	}
	return lastDelivered, nil
}
