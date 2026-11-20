// Package ratelimit — per-IP token bucket для админ-эндпоинтов.
//
// Зачем: /debug/event проверяет X-Admin-Key constant-time compare, но без
// ограничения частоты запросов брутфорс ключа с LAN при опубликованном
// порте остаётся бесплатным, а каждая неудачная авторизация молча
// исчезает в 401 (внешний аудит 20.11: P2-25, НФТ-3). Middleware режет
// частоту ДО авторизации и даёт единый лог-след неудачных попыток.
package ratelimit

import (
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Порог inline-чистки: на столько обращений к allow делается проход по
// карте с удалением записей, не видимых дольше idleTTL. Рост карты
// ограничен числом активных клиентов за окно; фоновая горутина-джанитор
// не нужна — limiter не держит ни одного ресурса, и gc/выход процесса
// закрывают его сами. Переменные (не const) — чтобы тесты могли задать
// мелкие значения без ожидания реального окна.
var (
	sweepEvery = uint64(1024)
	idleTTL    = 10 * time.Minute
)

// PerIP — token bucket на каждый клиентский IP. Конструируется
// NewPerIP; нулевое значение не годится (карта не инициализирована).
type PerIP struct {
	mu      sync.Mutex
	limit   rate.Limit
	burst   int
	entries map[string]*entry
	seen    uint64 // счётчик обращений к allow для периодической чистки
}

type entry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewPerIP делает лимитер: limit — разрешённая частота (событий/сек),
// burst — допустимый мгновенный всплеск.
func NewPerIP(limit rate.Limit, burst int) *PerIP {
	return &PerIP{
		limit:   limit,
		burst:   burst,
		entries: make(map[string]*entry),
	}
}

// NewPerMinute — конструктор «N событий в минуту на IP»: bucket с
// burst=N покрывает всплеск ручных демо-запросов и при этом режет
// равномерный перебор ключа. Конфиг в «в минуту» удобнее для ops, чем
// events/сек с дробями.
func NewPerMinute(perMin int) *PerIP {
	return NewPerIP(rate.Limit(float64(perMin)/60), perMin)
}

// Middleware оборачивает handler: превышение лимита для IP клиента —
// 429 с Retry-After и WARN в лог. Лимит проверяется до авторизации:
// цель — сделать брутфорс дорогим, а не усложнить честный клиент.
func (p *PerIP) Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := ClientIP(r)
		if !p.allow(ip) {
			slog.Warn("rate limit exceeded", "client_ip", ip, "path", r.URL.Path)
			w.Header().Set("Retry-After", "60")
			http.Error(w, `{"error":"rate_limited"}`, http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

// allow тратит токен bucket'а этого IP. Ложь — токенов нет.
func (p *PerIP) allow(ip string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.seen++
	if p.seen%sweepEvery == 0 {
		p.sweepLocked()
	}

	e, ok := p.entries[ip]
	if !ok {
		e = &entry{limiter: rate.NewLimiter(p.limit, p.burst)}
		p.entries[ip] = e
	}
	e.lastSeen = time.Now()
	return e.limiter.Allow()
}

// sweepLocked удаляет записи клиентов, не обращавшихся дольше idleTTL.
// Вызывается только под мьютексом.
func (p *PerIP) sweepLocked() {
	cutoff := time.Now().Add(-idleTTL)
	for ip, e := range p.entries {
		if e.lastSeen.Before(cutoff) {
			delete(p.entries, ip)
		}
	}
}

// ClientIP достаёт IP клиента из RemoteAddr. X-Forwarded-For не читается
// сознательно: заголовок подделывается любым клиентом, а доверенного
// прокси перед сервисом пока нет. Когда появится TLS-прокси (nginx),
// здесь надо будет брать адрес из XFF только при запросе от доверенного
// источника — иначе лимитер начнёт лимитировать прокси, а не клиентов.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// Не host:port (например, тестовый RemoteAddr без порта) —
		// используем как есть, это единственная доступная идентичность.
		return r.RemoteAddr
	}
	return host
}
