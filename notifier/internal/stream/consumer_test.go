package stream

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/redis/go-redis/v9"

	"notifier/internal/metrics"
	"notifier/internal/model"
)

const validPayload = `{"event_id":"550e8400-e29b-41d4-a716-446655440000","id":123,"source":"mock",
	"deal_type":"rent","country":"BY","city":"Minsk","rooms":1,"price":380,"currency":"USD",
	"price_usd":380,"address":null,"url":"https://example.com/123",
	"created_at":"2026-10-28T10:00:00Z"}`

// fakeRedis — сценарный двойник Redis: первую пачку сообщений выдаёт XREADGROUP,
// дальше пусто. Подтверждения и dead-letter пишет в логи для проверок.
// Состояние стрима (streamIDs/pendingIDs/lastDelivered) используется тестами
// очистки (reaper_test.go).
type fakeRedis struct {
	mu        sync.Mutex
	groupErr  error
	msgs      []redis.XMessage
	acked     []string
	dead      []redis.XAddArgs
	xaddErr   error
	readCalls int

	streamIDs     []string // содержимое стрима, отсортировано по ID
	pendingIDs    []string // PEL группы (в работе / не подтверждены)
	lastDelivered string   // last-delivered-id группы
	deleted       []string // записи, удалённые XDel
	streamLen     int64    // длина стрима для XLEN (мониторинг мягкого порога, ТЗ 3.6)

	// onGroupInfo вызывается внутри XInfoGroups (под мьютексом) — имитирует
	// диспетчера, успевшего прочитать пачку между XPENDING и XInfoGroups.
	onGroupInfo func()
}

func (f *fakeRedis) XGroupCreateMkStream(ctx context.Context, stream, group, start string) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(ctx)
	if f.groupErr != nil {
		cmd.SetErr(f.groupErr)
	} else {
		cmd.SetVal("OK")
	}
	return cmd
}

func (f *fakeRedis) XReadGroup(ctx context.Context, a *redis.XReadGroupArgs) *redis.XStreamSliceCmd {
	f.mu.Lock()
	f.readCalls++
	msgs := f.msgs
	f.msgs = nil
	f.mu.Unlock()
	cmd := redis.NewXStreamSliceCmd(ctx, a)
	if len(msgs) > 0 {
		cmd.SetVal([]redis.XStream{{Stream: StreamName, Messages: msgs}})
		return cmd
	}
	if ctx.Err() != nil {
		cmd.SetErr(ctx.Err())
		return cmd
	}
	cmd.SetErr(redis.Nil) // имитация истечения Block
	return cmd
}

func (f *fakeRedis) XAutoClaim(ctx context.Context, a *redis.XAutoClaimArgs) *redis.XAutoClaimCmd {
	cmd := redis.NewXAutoClaimCmd(ctx, a)
	cmd.SetVal(nil, "0-0")
	return cmd
}

func (f *fakeRedis) XAck(ctx context.Context, stream, group string, ids ...string) *redis.IntCmd {
	f.mu.Lock()
	f.acked = append(f.acked, ids...)
	f.mu.Unlock()
	cmd := redis.NewIntCmd(ctx, stream, group, ids)
	cmd.SetVal(int64(len(ids)))
	return cmd
}

func (f *fakeRedis) XAdd(ctx context.Context, a *redis.XAddArgs) *redis.StringCmd {
	f.mu.Lock()
	f.dead = append(f.dead, *a)
	f.mu.Unlock()
	cmd := redis.NewStringCmd(ctx, a)
	if f.xaddErr != nil {
		cmd.SetErr(f.xaddErr)
	} else {
		cmd.SetVal("dead-1")
	}
	return cmd
}

func (f *fakeRedis) XLen(ctx context.Context, stream string) *redis.IntCmd {
	f.mu.Lock()
	n := f.streamLen
	f.mu.Unlock()
	cmd := redis.NewIntCmd(ctx, stream)
	cmd.SetVal(n)
	return cmd
}

func (f *fakeRedis) ackedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.acked...)
}

// XPending — summary-форма: Count и границы PEL (Lower — минимальный ID).
func (f *fakeRedis) XPending(ctx context.Context, stream, group string) *redis.XPendingCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	cmd := redis.NewXPendingCmd(ctx)
	p := &redis.XPending{Consumers: map[string]int64{}}
	if len(f.pendingIDs) > 0 {
		p.Count = int64(len(f.pendingIDs))
		p.Lower = f.pendingIDs[0]
		p.Higher = f.pendingIDs[len(f.pendingIDs)-1]
	}
	cmd.SetVal(p)
	return cmd
}

// XInfoGroups отдаёт группу notifier с её last-delivered-id.
func (f *fakeRedis) XInfoGroups(ctx context.Context, stream string) *redis.XInfoGroupsCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	cmd := redis.NewXInfoGroupsCmd(ctx, stream)
	cmd.SetVal([]redis.XInfoGroup{{Name: GroupName, LastDeliveredID: f.lastDelivered}})
	if f.onGroupInfo != nil {
		f.onGroupInfo()
	}
	return cmd
}

// XRange фильтрует стрим по диапазону: "-" — с начала, "(" — исключающая
// граница (Redis 6.2+), как в реальном XRANGE.
func (f *fakeRedis) XRange(ctx context.Context, stream, start, stop string) *redis.XMessageSliceCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	cmd := redis.NewXMessageSliceCmd(ctx)
	var out []redis.XMessage
	for _, id := range f.streamIDs {
		if !inRange(id, start, stop) {
			continue
		}
		out = append(out, redis.XMessage{ID: id, Values: map[string]any{"data": "{}"}})
	}
	cmd.SetVal(out)
	return cmd
}

// XDel удаляет записи из стрима; PEL не трогает — как реальный Redis
// (тест очистки проверяет, что pending после XDEL не меняется).
func (f *fakeRedis) XDel(ctx context.Context, stream string, ids ...string) *redis.IntCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	cmd := redis.NewIntCmd(ctx)
	removed := map[string]bool{}
	for _, id := range ids {
		removed[id] = true
	}
	var rest []string
	for _, id := range f.streamIDs {
		if removed[id] {
			f.deleted = append(f.deleted, id)
			continue
		}
		rest = append(rest, id)
	}
	f.streamIDs = rest
	cmd.SetVal(int64(len(ids)))
	return cmd
}

// inRange проверяет, что id попадает в диапазон XRANGE: "-" — с начала,
// "+" — до конца; префикс "(" задаёт исключающую границу (Redis 6.2+).
// Сравнение — как пар (мс, seq), лексикография строк потока не годится.
func inRange(id, start, stop string) bool {
	min := start
	minExclusive := false
	if s, ok := strings.CutPrefix(start, "("); ok {
		min, minExclusive = s, true
	}
	max := stop
	maxExclusive := false
	if s, ok := strings.CutPrefix(stop, "("); ok {
		max, maxExclusive = s, true
	}
	ge := min == "-" || compareStreamIDs(id, min) > 0 || (compareStreamIDs(id, min) == 0 && !minExclusive)
	le := max == "+" || compareStreamIDs(id, max) < 0 || (compareStreamIDs(id, max) == 0 && !maxExclusive)
	return ge && le
}

func (f *fakeRedis) deadCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.dead)
}

func (f *fakeRedis) deadValues() []redis.XAddArgs {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]redis.XAddArgs(nil), f.dead...)
}

// runConsumer запускает консьюмер, ждёт готовности условия и останавливает его.
func runConsumer(t *testing.T, rdb *fakeRedis, handle Handler, ready func() bool) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- NewConsumer(rdb, "test-1", 2, lenWarnThreshold, handle).Run(ctx) }()

	deadline := time.Now().Add(3 * time.Second)
	for !ready() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("consumer did not stop after cancel")
		return nil
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestConsumerDeliversAndAcks(t *testing.T) {
	rdb := &fakeRedis{msgs: []redis.XMessage{
		{ID: "1-1", Values: map[string]any{"data": validPayload}},
	}}

	handled := make(chan model.Event, 1)
	err := runConsumer(t, rdb, func(_ context.Context, e model.Event) error {
		handled <- e
		return nil
	}, func() bool { return len(rdb.ackedIDs()) == 1 })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	select {
	case e := <-handled:
		if e.ID != 123 || e.EventID == "" {
			t.Errorf("unexpected event: %+v", e)
		}
	default:
		t.Fatal("handler not called")
	}
	if got := rdb.ackedIDs(); len(got) != 1 || got[0] != "1-1" {
		t.Errorf("acked = %v, want [1-1]", got)
	}
	if rdb.deadCount() != 0 {
		t.Errorf("dead-letter = %d, want 0", rdb.deadCount())
	}
}

func TestConsumerDeadLetterOnBadPayload(t *testing.T) {
	rdb := &fakeRedis{msgs: []redis.XMessage{
		{ID: "2-1", Values: map[string]any{"data": `{oops`}},
	}}

	handlerCalled := false
	err := runConsumer(t, rdb, func(context.Context, model.Event) error { handlerCalled = true; return nil },
		func() bool { return rdb.deadCount() == 1 && len(rdb.ackedIDs()) == 1 })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if handlerCalled {
		t.Error("handler must not be called for bad payload")
	}
	if got := rdb.ackedIDs(); len(got) != 1 || got[0] != "2-1" {
		t.Errorf("acked = %v, want [2-1]", got)
	}
	dead := rdb.deadValues()
	if len(dead) != 1 || dead[0].Stream != DeadStream {
		t.Fatalf("dead = %+v", dead)
	}
	vals, _ := dead[0].Values.(map[string]any)
	if vals["reason"] == "" || vals["data"] != `{oops` {
		t.Errorf("dead values = %v", vals)
	}
	// Телеметрия dead-letter (п.7a ревью #125): счётчик растёт на битом payload.
	if got := testutil.ToFloat64(metrics.DeadLetter); got < 1 {
		t.Fatalf("DeadLetter = %v, want > 0 (битый payload уложен в listings.dead)", got)
	}
}

func TestConsumerDeadLetterOnMissingData(t *testing.T) {
	rdb := &fakeRedis{msgs: []redis.XMessage{
		{ID: "3-1", Values: map[string]any{"other": "x"}},
	}}
	err := runConsumer(t, rdb, func(context.Context, model.Event) error { return nil },
		func() bool { return rdb.deadCount() == 1 })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestConsumerKeepsPendingWhenDeadLetterFails(t *testing.T) {
	rdb := &fakeRedis{
		msgs:    []redis.XMessage{{ID: "4-1", Values: map[string]any{"data": `{oops`}}},
		xaddErr: errors.New("redis unavailable"),
	}
	err := runConsumer(t, rdb, func(context.Context, model.Event) error { return nil },
		func() bool { return true }) // сразу останавливаем
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// XADD не прошёл — подтверждать нельзя, запись должна остаться в pending.
	if got := rdb.ackedIDs(); len(got) != 0 {
		t.Errorf("acked = %v, want none (dead-letter write failed)", got)
	}
}

// TestConsumerKeepsPendingWhenHandlerFails — issue #53 (ТЗ 4.3): ошибка
// хендлера — временный инфраструктурный сбой (Postgres/Redis), XACK не
// выполняется, запись остаётся в pending и повторяется через XAUTOCLAIM.
func TestConsumerKeepsPendingWhenHandlerFails(t *testing.T) {
	rdb := &fakeRedis{msgs: []redis.XMessage{
		{ID: "6-1", Values: map[string]any{"data": validPayload}},
	}}
	handled := make(chan struct{}, 1)
	err := runConsumer(t, rdb, func(context.Context, model.Event) error {
		handled <- struct{}{}
		return errors.New("db down")
	}, func() bool {
		select {
		case <-handled:
			return true
		default:
			return false
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Небольшая пауза: ошибочный XACK (если бы он был) успел бы пройти
	// до проверки — в корректной реализации его просто нет.
	time.Sleep(50 * time.Millisecond)
	if got := rdb.ackedIDs(); len(got) != 0 {
		t.Errorf("acked = %v, want none (handler error must keep record pending)", got)
	}
}

func TestConsumerToleratesExistingGroup(t *testing.T) {
	rdb := &fakeRedis{
		groupErr: errors.New("BUSYGROUP Consumer Group name already exists"),
		msgs:     []redis.XMessage{{ID: "5-1", Values: map[string]any{"data": validPayload}}},
	}
	err := runConsumer(t, rdb, func(context.Context, model.Event) error { return nil },
		func() bool { return len(rdb.ackedIDs()) == 1 })
	if err != nil {
		t.Fatalf("Run must tolerate BUSYGROUP: %v", err)
	}
}

func TestConsumerGroupCreateFailureIsFatal(t *testing.T) {
	rdb := &fakeRedis{groupErr: errors.New("WRONGTYPE bad kind")}
	err := runConsumer(t, rdb, func(context.Context, model.Event) error { return nil }, func() bool { return true })
	if err == nil {
		t.Fatal("group create failure must be returned as error")
	}
}

func TestConsumerName(t *testing.T) {
	if ConsumerName() == "" {
		t.Fatal("consumer name must not be empty")
	}
}

// syncBuf — потокобезопасный writer для перехвата стандартного лога:
// readLoop пишет из своей горутины, тест читает из ready().
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// TestStreamLenWarnStateHysteresis — чистая функция гистерезиса WARN
// (ТЗ 3.6: мягкий порог 10 000, без удаления): WARN один раз при выходе
// за порог, дальше молчание, пока длина не упадёт ниже reset-порога 9000.
func TestStreamLenWarnStateHysteresis(t *testing.T) {
	// Ниже порога — молчание, гистерезис не взводится.
	if warn, w := streamLenWarnState(lenWarnThreshold-1, false, lenWarnThreshold); warn || w {
		t.Fatalf("len < порога: warn=%v, warned=%v; want false, false", warn, w)
	}
	// Ровно порог — уже превышение (>=).
	if warn, w := streamLenWarnState(lenWarnThreshold, false, lenWarnThreshold); !warn || !w {
		t.Fatalf("len = порога: warn=%v, warned=%v; want true, true", warn, w)
	}
	// Пока warned и длина ещё выше reset — повторных WARN нет (не спамим).
	if warn, w := streamLenWarnState(lenWarnThreshold+100, true, lenWarnThreshold); warn || !w {
		t.Fatalf("длинный стрим после WARN: warn=%v, warned=%v; want false, true", warn, w)
	}
	// Длина упала ниже reset — гистерезис снят (без WARN в этот проход).
	if warn, w := streamLenWarnState(streamLenWarnReset-1, true, lenWarnThreshold); warn || w {
		t.Fatalf("len < reset: warn=%v, warned=%v; want false, false", warn, w)
	}
	// После снятия гистерезиса новый выход за порог снова предупреждает.
	if warn, w := streamLenWarnState(lenWarnThreshold, false, lenWarnThreshold); !warn || !w {
		t.Fatalf("повторный выход за порог: warn=%v, warned=%v; want true, true", warn, w)
	}
}

// TestConsumerWarnsOnStreamLenThreshold — интеграционный: readLoop с фейковым
// Redis, длина стрима за мягким порогом (ТЗ 3.6) — в логе появляется WARN
// с текущим значением и именем consumer'а, обработка при этом не ломается.
func TestConsumerWarnsOnStreamLenThreshold(t *testing.T) {
	// Захват slog-логера через прод-JSON-хендлер (карточка 02): тест
	// фиксирует прод-формат WARN, а не fallback-рендеринг дефолтного
	// хендлера. syncBuf обязателен — readLoop пишет из своей горутины.
	sbuf := &syncBuf{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(sbuf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	rdb := &fakeRedis{streamLen: lenWarnThreshold + 1}
	err := runConsumer(t, rdb, func(context.Context, model.Event) error { return nil },
		func() bool { return strings.Contains(sbuf.String(), "soft threshold") })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	logged := sbuf.String()
	// JSON-формат WARN (card 02 prod-10of10): поле length — текущая длина
	// стрима за мягким порогом (значения как в прежней проверке).
	if !strings.Contains(logged, fmt.Sprintf(`"length":%d`, lenWarnThreshold+1)) {
		t.Errorf("в логе нет текущей длины стрима: %q", logged)
	}
	if !strings.Contains(logged, `"consumer":"test-1"`) {
		t.Errorf("в логе нет ID воркера: %q", logged)
	}
	if strings.Contains(logged, "XLEN") || strings.Contains(logged, "xlen") {
		t.Errorf("ошибка XLEN не должна попадать в лог при успехе: %q", logged)
	}
}

// TestConsumerStaysSilentBelowStreamLenThreshold — длина стрима ниже мягкого
// порога (ТЗ 3.6): WARN не логируется.
func TestConsumerStaysSilentBelowStreamLenThreshold(t *testing.T) {
	sbuf := &syncBuf{}
	old := log.Writer()
	log.SetOutput(sbuf)
	defer log.SetOutput(old)

	rdb := &fakeRedis{streamLen: lenWarnThreshold - 1}
	err := runConsumer(t, rdb, func(context.Context, model.Event) error { return nil },
		func() bool { return true }) // ждём штатную остановку по cancel
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(sbuf.String(), "soft threshold") {
		t.Errorf("WARN при длине ниже порога не должен логироваться: %q", sbuf.String())
	}
}
