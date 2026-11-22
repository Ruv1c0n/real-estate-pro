// Package ratelimit — per-IP token bucket для админ-эндпоинтов.
//
// Зачем: /debug/event проверяет X-Admin-Key constant-time compare, но без
// ограничения частоты запросов брутфорс ключа с LAN при опубликованном
// порте остаётся бесплатным, а каждая неудачная авторизация молча
// исчезает в 401 (внешний аудит 20.11: P2-25, НФТ-3). Middleware режет
// частоту ДО авторизации, даёт единый лог-след неудачных попыток и
// прогрессивно банит перебор: после 5 неудач подряд — 5 минут блокировки
// с удвоением срока до потолка в 60 минут.
package ratelimit

import (
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"notifier/internal/httpx"
)

const (
	// Дефолты полей PerIP: периодичность чистки, срок жизни «спящей»
	// записи и верхняя граница размера карты.
	defaultSweepEvery = uint64(1024)
	defaultIdleTTL    = 10 * time.Minute
	defaultMaxEntries = 8192

	// Прогрессивный бан за брутфорс авторизации: после authFailBanAfter
	// неудач подряд IP блокируется на banBase, каждая следующая неудача
	// удваивает срок до потолка banMax.
	authFailBanAfter = 5
	banBase          = 5 * time.Minute
	banMax           = 60 * time.Minute

	// Fallback-лимитер: один бакет на все IP сверх maxEntries (всплеск
	// новых адресов от rot-пула лимитировать по одному бессмысленно, но
	// и отказывать им всем нельзя — среди новых есть честные клиенты).
	fallbackPerMin = 120
)

// PerIP — token bucket на каждый клиентский IP, счётчик неудач авторизации
// и прогрессивный бан за брутфорс. Ключ бакетирования: IPv4 — сам адрес,
// IPv6 — /64-префикс (privacy extensions ротируют /128). Конструируется
// NewPerMinute; нулевое значение не годится (карта не инициализирована).
type PerIP struct {
	mu     sync.Mutex
	limit  rate.Limit
	burst  int
	perMin int // «N событий/мин» из конфига — из него считается Retry-After

	entries map[string]*entry
	seen    uint64 // счётчик обращений к admit для периодической чистки

	// Tuning-поля: конструктор задаёт дефолты, тесты правят напрямую
	// (пакет тот же), глобальных переменных больше нет.
	sweepEvery     uint64        // каждые N обращений к admit — проход чистки
	idleTTL        time.Duration // записи молчаще TTL удаляются
	maxEntries     int           // cap карты: сверх него IP обслуживает fallback
	fallback       *rate.Limiter // общий бакет для «новых» IP сверх cap'а
	fallbackPerMin int           // лимит fallback'а для вычисления Retry-After

	// Флаги «WARN уже написан»: отказ не должен флудить журнал при
	// брутфорсе.
	overflowWarned     bool // WARN о переполнении карты — один раз навсегда
	fallbackWarnLogged bool // WARN об отказе fallback'а — раз в окно
}

type entry struct {
	limiter    *rate.Limiter
	lastSeen   time.Time
	authFails  int       // последовательные неудачи авторизации (бан по порогу)
	banUntil   time.Time // нулевое время — бан не активен
	warnLogged bool      // WARN 429 уже написан в текущем окне отказов
}

// NewPerMinute — конструктор «N событий в минуту на IP»: bucket с
// burst=N покрывает всплеск ручных демо-запросов и при этом режет
// равномерный перебор ключа. Конфиг в «в минуту» удобнее для ops, чем
// events/сек с дробями. Retry-After при отказе считается из того же N;
// tuning-поля получают дефолты, тесты правят их напрямую.
func NewPerMinute(perMin int) *PerIP {
	return &PerIP{
		limit:          rate.Limit(float64(perMin) / 60),
		burst:          perMin,
		perMin:         perMin,
		entries:        make(map[string]*entry),
		sweepEvery:     defaultSweepEvery,
		idleTTL:        defaultIdleTTL,
		maxEntries:     defaultMaxEntries,
		fallback:       rate.NewLimiter(rate.Limit(float64(fallbackPerMin)/60), fallbackPerMin),
		fallbackPerMin: fallbackPerMin,
	}
}

// admission — решение admit: пропустить или отказать с параметрами 429.
type admission struct {
	allowed    bool
	retryAfter int  // секунды до следующей попытки, минимум 1
	warn       bool // true — первый отказ в окне, middleware пишет WARN
}

// Middleware оборачивает handler: превышение лимита для IP клиента —
// 429 с Retry-After и WARN в лог (первый отказ в окне — флуд брутфорса
// не должен заваливать журнал). Бан за брутфорс авторизации проверяется
// ДО токен-бакета, а лимит — до авторизации: цель — сделать брутфорс
// дорогим, а не усложнить честный клиент.
func (p *PerIP) Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := httpx.ClientIP(r)
		a := p.admit(ip)
		if a.allowed {
			next(w, r)
			return
		}
		if a.warn {
			// Русский смысл стаба: WARN об отказе. Пишется только на первый
			// отказ в окне (см. admit) — флуд брутфорса не заваливает журнал.
			slog.Warn("rate limit exceeded", "client_ip", ip, "path", r.URL.Path)
		}
		w.Header().Set("Retry-After", strconv.Itoa(a.retryAfter))
		http.Error(w, `{"error":"rate_limited"}`, http.StatusTooManyRequests)
	}
}

// admit решает судьбу запроса клиента ip: активный бан — отказ с
// оставшимся сроком бана; иначе token bucket' записи; для IP сверх
// maxEntries — общий fallback-бакет. Вызывается только из Middleware.
func (p *PerIP) admit(ip string) admission {
	p.mu.Lock()
	defer p.mu.Unlock()

	bucket := bucketKey(ip)
	now := time.Now()

	p.seen++
	if p.seen%p.sweepEvery == 0 {
		p.sweepLocked(now)
	}

	e, ok := p.entries[bucket]

	// Бан ДО токен-бакета: переборщик не должен тратить токены, пока
	// ждёт отлуп. lastSeen обновляется и здесь, иначе чистка удалит
	// запись (и вместе с ней бан) во время затяжной осады.
	if ok && now.Before(e.banUntil) {
		e.lastSeen = now
		retry := retryFor(p.perMin)
		if left := int(e.banUntil.Sub(now).Seconds()) + 1; left > retry {
			retry = left // оставшееся время бана, если оно больше
		}
		warn := !e.warnLogged
		e.warnLogged = true
		return admission{retryAfter: retry, warn: warn}
	}

	if !ok {
		if len(p.entries) >= p.maxEntries {
			return p.admitFallbackLocked()
		}
		e = &entry{limiter: rate.NewLimiter(p.limit, p.burst)}
		p.entries[bucket] = e
	}
	e.lastSeen = now
	if e.limiter.Allow() {
		e.warnLogged = false // пропуск открывает новое окно WARN
		return admission{allowed: true}
	}
	warn := !e.warnLogged
	e.warnLogged = true
	return admission{retryAfter: retryFor(p.perMin), warn: warn}
}

// admitFallbackLocked обслуживает IP сверх cap'а карты общим бакетом:
// записи плодить нельзя — карта росла бы без границ, отказать всем —
// выкинуть честных клиентов. Вызывается только под мьютексом.
func (p *PerIP) admitFallbackLocked() admission {
	if !p.overflowWarned {
		// Русский смысл стаба: «карта переполнена (cap записей) — новые IP
		// обслуживает общий fallback-бакет».
		slog.Warn("rate limit: map overflow, new IPs served by shared fallback bucket", "max_entries", p.maxEntries)
		p.overflowWarned = true
	}
	if p.fallback.Allow() {
		p.fallbackWarnLogged = false
		return admission{allowed: true}
	}
	warn := !p.fallbackWarnLogged
	p.fallbackWarnLogged = true
	return admission{retryAfter: retryFor(p.fallbackPerMin), warn: warn}
}

// NoteAuthFailure отмечает неудачную авторизацию (неверный или
// отсутствующий X-Admin-Key) от клиента ip. После authFailBanAfter неудач
// подряд ip блокируется на banBase, каждая следующая неудача удваивает
// срок до потолка banMax (см. banDuration). Вызывается из auth-пути
// хендлера в main, к токенам отношения не имеет.
func (p *PerIP) NoteAuthFailure(ip string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	bucket := bucketKey(ip)
	e, ok := p.entries[bucket]
	if !ok {
		// Сверх cap'а карты новых записей не создаём (как в admit):
		// такие клиенты уже обслуживаются fallback-бакетом.
		if len(p.entries) >= p.maxEntries {
			return
		}
		e = &entry{limiter: rate.NewLimiter(p.limit, p.burst)}
		p.entries[bucket] = e
	}
	e.authFails++
	if e.authFails >= authFailBanAfter {
		e.banUntil = time.Now().Add(banDuration(e.authFails))
	}
}

// NoteAuthSuccess — верный ключ: счётчик неудач и активный бан
// обнуляются. Вызывается из auth-пути хендлера в main после успешной
// constant-time сверки ключа.
func (p *PerIP) NoteAuthSuccess(ip string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.entries[bucketKey(ip)]; ok {
		e.authFails = 0
		e.banUntil = time.Time{}
	}
}

// banDuration — срок бана за n-ю подряд неудачу: banBase * 2^(n-threshold)
// с потолком banMax. Цикл вместо сдвига — без переполнения при больших n.
func banDuration(fails int) time.Duration {
	d := banBase
	for i := authFailBanAfter; i < fails && d < banMax; i++ {
		d *= 2
	}
	if d > banMax {
		d = banMax
	}
	return d
}

// retryFor — Retry-After в секундах для лимита «perMin/мин»: ceil(60/perMin)
// — ровно пауза, за которую опустевший bucket гарантированно наберёт один
// токен. Минимум 1: клиенту нужен корректный заголовок при любом конфиге.
func retryFor(perMin int) int {
	r := (60 + perMin - 1) / perMin
	if r < 1 {
		r = 1
	}
	return r
}

// bucketKey — ключ бакетирования: для IPv4 сам адрес, для IPv6 — /64
// префикс сети. Privacy extensions (RFC 4941) ротируют младшие 64 бита,
// поэтому per-/128 лимит против них бессмысленен, а /64 заодно режет
// перебор со всей подсети клиента.
func bucketKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		// Не разбирается как IP (unix-сокет, тестовый RemoteAddr) —
		// используем строку как есть: это единственная идентичность.
		return ip
	}
	addr = addr.Unmap() // ::ffff:203.0.113.5 — IPv4-клиент, а не начало /64
	if addr.Is4() {
		return addr.String()
	}
	prefix, err := addr.Prefix(64)
	if err != nil {
		// Недостижимо для разобранного адреса (IPv6 длиннее 64 бит);
		// подстрахуемся строкой без бакетирования.
		return ip
	}
	return prefix.String()
}

// sweepLocked удаляет записи клиентов, не обращавшихся дольше idleTTL.
// Бан «живёт» ровно пока запись в карте: при активной осаде lastSeen
// обновляется на каждом отказе, а затянувшаяся пауза честно сбрасывает
// и бан, и счётчик неудач — вечное хранение банов неограниченно растит
// память. Вызывается только под мьютексом.
func (p *PerIP) sweepLocked(now time.Time) {
	cutoff := now.Add(-p.idleTTL)
	for b, e := range p.entries {
		if e.lastSeen.Before(cutoff) {
			delete(p.entries, b)
		}
	}
}
