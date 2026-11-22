package ratelimit

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestPerIPAllowsBurstThenDenies(t *testing.T) {
	// 3 события в минуту на IP: burst=3 — три запроса подряд проходят,
	// четвёртый упирается в пустой bucket (аудит P2-25).
	p := NewPerMinute(3)
	for i := 0; i < 3; i++ {
		if !p.admit("192.0.2.1").allowed {
			t.Fatalf("запрос %d отклонён: bucket должен позволить burst=3", i+1)
		}
	}
	if a := p.admit("192.0.2.1"); a.allowed {
		t.Fatal("4-й запрос подряд прошёл: лимит 3/мин на IP не работает")
	}
	// Другой IP — свой bucket: чужой перебор не съедает лимит честного клиента.
	if !p.admit("192.0.2.2").allowed {
		t.Fatal("второй IP отклонён: bucket'ы должны быть независимыми")
	}
}

func TestPerIPDenialDoesNotConsumeToken(t *testing.T) {
	// Отказ не должен сбрасывать bucket: после паузы, дающей один токен,
	// ровно один запрос проходит. Если бы отказы тратили токены, клиент
	// под лимитом навсегда остался бы заблокированным. Конструктора
	// NewPerIP больше нет (уборка API): частота и burst задаются полями.
	p := NewPerMinute(1)
	p.limit, p.burst = rate.Limit(100), 1 // высокая частота: 1 токен за 10 мс
	if !p.admit("192.0.2.9").allowed {
		t.Fatal("первый запрос в burst=1 должен пройти")
	}
	if p.admit("192.0.2.9").allowed {
		t.Fatal("второй мгновенный запрос должен быть отклонён (burst=1)")
	}
	time.Sleep(15 * time.Millisecond)
	if !p.admit("192.0.2.9").allowed {
		t.Fatal("после пополнения bucket'а запрос должен пройти")
	}
}

func TestMiddlewareReturns429WithRetryAfter(t *testing.T) {
	p := NewPerMinute(1)
	handler := p.Middleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/debug/event", nil)
	req.RemoteAddr = "192.0.2.10:1234"

	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("первый запрос вернул %d, ожидается 200", rec.Code)
	}

	// Захват стандартного логера: WARN об отказе обязан содержать IP, но
	// один раз на окно — иначе брутфорс флудом заваливает журнал (п.3).
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	rec = httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("второй запрос вернул %d, ожидается 429", rec.Code)
	}
	// Retry-After вычисляется из конфигурации (п.4): при perMin=1 bucket
	// набирает токен за 60 сек — именно это и уходит в заголовок.
	if ra := rec.Header().Get("Retry-After"); ra != "60" {
		t.Fatalf("Retry-After = %q, ожидается 60 (ceil(60/perMin), perMin=1)", ra)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"rate_limited"`) {
		t.Fatalf("тело 429 без json-ошибки: %q", body)
	}
	if n := strings.Count(buf.String(), "192.0.2.10"); n != 1 {
		t.Fatalf("IP встречается в логе %d раз, ожидается 1 (WARN только на первый 429): %q", n, buf.String())
	}
}

func TestRetryAfterComputedFromLimit(t *testing.T) {
	// НЕ захардкоженные «60»: при perMin=2 токен появляется раз в 30 сек.
	p := NewPerMinute(2)
	for i := 0; i < 2; i++ {
		if !p.admit("192.0.2.61").allowed {
			t.Fatalf("запрос %d отклонён: burst=2 должен позволить", i+1)
		}
	}
	a := p.admit("192.0.2.61")
	if a.allowed {
		t.Fatal("третий запрос должен быть отклонён (лимит 2/мин)")
	}
	if a.retryAfter != 30 {
		t.Fatalf("retryAfter = %d, ожидается 30 (ceil(60/2))", a.retryAfter)
	}
}

func TestMiddlewareWarnsOncePerWindow(t *testing.T) {
	// П.3 ревью: WARN на 429 — один раз на IP в окно отказов, сброс —
	// пропуск запроса. Бакет подменяется напрямую, чтобы отказы были
	// детерминированы без снов.
	p := NewPerMinute(1)
	handler := p.Middleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodPost, "/debug/event", nil)
	req.RemoteAddr = "192.0.2.62:8080"

	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	// Первый запрос — пропуск: WARN нет.
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("первый запрос вернул %d, ожидается 200", rec.Code)
	}

	denyAll := func() {
		p.mu.Lock()
		p.entries[bucketKey("192.0.2.62")].limiter = rate.NewLimiter(0, 0)
		p.mu.Unlock()
	}
	allowAll := func() {
		p.mu.Lock()
		p.entries[bucketKey("192.0.2.62")].limiter = rate.NewLimiter(rate.Inf, 1)
		p.mu.Unlock()
	}

	denyAll()
	handler(httptest.NewRecorder(), req) // 429 → WARN (первый отказ в окне)
	handler(httptest.NewRecorder(), req) // 429 → без WARN (флуд не пишем)

	allowAll()
	handler(httptest.NewRecorder(), req) // пропуск → окно WARN закрылось

	denyAll()
	handler(httptest.NewRecorder(), req) // снова 429 → новый WARN

	if n := strings.Count(buf.String(), "192.0.2.62"); n != 2 {
		t.Fatalf("IP встречается в логе %d раз, ожидается 2 (по одному на окно отказов): %q", n, buf.String())
	}
}

func TestSweepDropsIdleEntries(t *testing.T) {
	// Хуки чистки — поля структуры (п.6 ревью): без глобалей и t.Cleanup.
	p := NewPerMinute(100)
	p.sweepEvery = 4
	p.idleTTL = time.Millisecond

	p.admit("192.0.2.20")
	p.admit("192.0.2.21")
	time.Sleep(5 * time.Millisecond) // обе записи старше idleTTL

	// sweepEvery=4: четыре обращения к разным IP запускают чистку,
	// которая обязана убрать протухшие записи.
	for i := 0; i < 6; i++ {
		p.admit("192.0.2.30")
	}

	p.mu.Lock()
	got := len(p.entries)
	p.mu.Unlock()
	if got != 1 {
		t.Fatalf("после чистки в карте %d записей, ожидается 1 (только активный IP)", got)
	}
}

func TestAuthFailuresBelowThresholdDoNotBan(t *testing.T) {
	// Бакет с запасом (100/мин): отказ admit при живых токенах может
	// означать только бан, значит бан ещё не включился.
	p := NewPerMinute(100)
	for i := 0; i < authFailBanAfter-1; i++ {
		p.NoteAuthFailure("192.0.2.70")
	}
	if a := p.admit("192.0.2.70"); !a.allowed {
		t.Fatalf("после %d неудач подряд бан ещё не нужен, но запрос отклонён", authFailBanAfter-1)
	}
}

func TestAuthFailureBanProgressiveDoubles(t *testing.T) {
	// П.1 ревью: 5-я неудача подряд включает бан на 5 минут, каждая
	// следующая удваивает срок, потолок 60 минут. Сроки проверяем через
	// admit Retry-After: свежий бан на N минут даёт ~N*60 сек.
	p := NewPerMinute(100)
	for i := 0; i < authFailBanAfter; i++ {
		p.NoteAuthFailure("192.0.2.71")
	}
	for _, wantMin := range []int{5, 10, 20, 40, 60, 60} {
		a := p.admit("192.0.2.71")
		if a.allowed {
			t.Fatalf("бан должен быть активен (неудач уже %d и больше)", authFailBanAfter)
		}
		if a.retryAfter < wantMin*60-10 || a.retryAfter > wantMin*60+1 {
			t.Fatalf("retryAfter = %d сек, ожидается около %d мин (прогрессивный бан)", a.retryAfter, wantMin)
		}
		p.NoteAuthFailure("192.0.2.71") // следующая неудача — срок удваивается
	}
}

func TestAuthSuccessResetsFailureCounterAndBan(t *testing.T) {
	p := NewPerMinute(100)
	for i := 0; i < authFailBanAfter; i++ {
		p.NoteAuthFailure("192.0.2.72")
	}
	if a := p.admit("192.0.2.72"); a.allowed {
		t.Fatal("после 5 неудач подряд бан должен быть активен")
	}
	// Верный ключ обнуляет и счётчик, и бан.
	p.NoteAuthSuccess("192.0.2.72")
	if a := p.admit("192.0.2.72"); !a.allowed {
		t.Fatal("успешная авторизация обязана снять бан")
	}
	// Счётчик именно обнулён: снова нужен полный порог, а не первая неудача.
	for i := 0; i < authFailBanAfter-1; i++ {
		p.NoteAuthFailure("192.0.2.72")
	}
	if a := p.admit("192.0.2.72"); !a.allowed {
		t.Fatalf("после сброса счётчика %d неудач не должны банить", authFailBanAfter-1)
	}
}

func TestMiddlewareBannedIPGets429WithBanRetryAfter(t *testing.T) {
	// Сквозной путь: забаненный IP получает 429 через middleware с
	// Retry-After = оставшееся время свежего 5-минутного бана.
	p := NewPerMinute(100)
	handler := p.Middleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	for i := 0; i < authFailBanAfter; i++ {
		p.NoteAuthFailure("192.0.2.73")
	}
	req := httptest.NewRequest(http.MethodPost, "/debug/event", nil)
	req.RemoteAddr = "192.0.2.73:1111"
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("забаненный IP получил %d, ожидается 429", rec.Code)
	}
	ra, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil || ra < 5*60-10 || ra > 5*60+1 {
		t.Fatalf("Retry-After = %q (%d, %v), ожидается оставшееся время бана ~300 сек", rec.Header().Get("Retry-After"), ra, err)
	}
}

func TestBucketKeyIPv6Prefix64(t *testing.T) {
	// П.2 ревью: privacy extensions ротируют /128 — ключ обязан быть /64.
	a := bucketKey("2001:db8:1:2::1")
	if a != "2001:db8:1:2::/64" {
		t.Fatalf("bucketKey IPv6 = %q, ожидается /64 префикс", a)
	}
	if b := bucketKey("2001:db8:1:2:ffff:ffff:ffff:ffff"); b != a {
		t.Fatalf("адреса одной /64 разъехались по бакетам: %q != %q", a, b)
	}
	// IPv4 бакетируется самим адресом.
	if k := bucketKey("192.0.2.5"); k != "192.0.2.5" {
		t.Fatalf("bucketKey IPv4 = %q, ожидается сам адрес", k)
	}
	// IPv4 в IPv6-обёртке — тот же IPv4-клиент, а не начало /64.
	if k := bucketKey("::ffff:192.0.2.5"); k != "192.0.2.5" {
		t.Fatalf("bucketKey 4-in-6 = %q, ожидается развёрнутый IPv4", k)
	}
}

func TestIPv6Same64SharesBucket(t *testing.T) {
	// Ротация интерфейсной части (/128) не должна обходить лимит.
	p := NewPerMinute(1)
	if !p.admit("2001:db8:1:2::1").allowed {
		t.Fatal("первый запрос от /64 должен пройти")
	}
	if p.admit("2001:db8:1:2:dead:beef:0:1").allowed {
		t.Fatal("ротация /128 обошла лимит: бакет должен быть общим на /64")
	}
	p.mu.Lock()
	n := len(p.entries)
	p.mu.Unlock()
	if n != 1 {
		t.Fatalf("в карте %d записей, ожидается 1 (одна /64 — один бакет)", n)
	}
}

func TestMapCapOverflowUsesFallback(t *testing.T) {
	// П.2 ревью: cap карты — новые записи не плодятся, «новые» IP идут
	// через общий fallback-бакет и всё же обслуживаются.
	p := NewPerMinute(1000)
	p.maxEntries = 3

	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	for _, ip := range []string{"192.0.2.80", "192.0.2.81", "192.0.2.82"} {
		if !p.admit(ip).allowed {
			t.Fatalf("%s отклонён при заполнении cap'а", ip)
		}
	}

	if a := p.admit("192.0.2.83"); !a.allowed {
		t.Fatal("новый IP при переполнении должен обслуживаться fallback'ом, а не отказом")
	}
	if !p.overflowWarned {
		t.Fatal("о переполнении карты должен быть WARN")
	}
	p.mu.Lock()
	n := len(p.entries)
	p.mu.Unlock()
	if n != 3 {
		t.Fatalf("карта выросла до %d записей при cap 3: переполнение не остановлено", n)
	}

	// Истощённый fallback: отказ с Retry-After по его конфигурации —
	// семантика 429 та же, лимит общий.
	p.fallback = rate.NewLimiter(0, 0)
	if a := p.admit("192.0.2.84"); a.allowed {
		t.Fatal("истощённый fallback обязан отклонять запросы новых IP")
	}
	if a := p.admit("192.0.2.84"); a.allowed || a.retryAfter != retryFor(p.fallbackPerMin) {
		t.Fatalf("fallback-отказ: retryAfter = %d, ожидается %d", a.retryAfter, retryFor(p.fallbackPerMin))
	}
	p.mu.Lock()
	n = len(p.entries)
	p.mu.Unlock()
	if n != 3 {
		t.Fatalf("после fallback-отказов карта выросла до %d записей", n)
	}
	if cnt := strings.Count(buf.String(), "переполнена"); cnt != 1 {
		t.Fatalf("WARN о переполнении встречается %d раз, ожидается 1 (флаг): %q", cnt, buf.String())
	}
}
