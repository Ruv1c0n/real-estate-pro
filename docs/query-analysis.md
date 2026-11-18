# Анализ запросов и индексов схемы core (НФТ-10, ТЗ 6.1)

Отвечает: Илья. Цель — проверить, что индексы миграции 0001 соответствуют
фильтрам из `docs/contracts.md` (`GET /api/v1/listings`), ещё **до**
появления кода Core API: запросы для анализа взяты прямо из контракта,
данные — синтетические. Менять схему по итогам не потребовалось; ниже
два пункта «на наблюдение», решение по ним — после реальных запросов
Тимура и замеров из `scripts/bench/` (PR #76, НФТ-10: p95 < 300 мс с кэшем,
< 500 мс без).

## Методика

- Среда: чистый контейнер `postgres:16`, схема только из миграции 0001
  (стек PR #81), без статистики реальной эксплуатации.
- Данные: `scripts/db/gen-analysis-data.py` — синтетические объявления
  с разбросом по 10 городам, типам сделки, комнатности, площадям и валютам;
  ~8% пар-дублей по `content_hash`; outbox с ~30% незавершённых заданий.
- Два масштаба:
  - **демо** — 561 объявление (порог НФТ-10: 500+), ~56 на город;
  - **стресс** — 54 000 объявлений, ~5 400 на город (×100 от порога ТЗ —
    проверка запаса прочности).
- Планы сняты `EXPLAIN (ANALYZE, TIMING OFF)`; числа ниже — типовой прогон,
  точные значения зависят от машины.

## Запросы (по contracts.md) и результаты

| # | Запрос (фильтры) | Демо (561) | Стресс (54k) | Используемый индекс |
|---|---|---|---|---|
| Q1 | `deal_type + city_id + price_usd BETWEEN`, сортировка `created_at DESC` | 0.14 мс | 0.4 мс | `listings_deal_city_price_idx` или `listings_created_at_idx` (обход с фильтром) |
| Q2 | страница по умолчанию (`created_at DESC LIMIT 20`) | мгновенно | мгновенно | `listings_created_at_idx` |
| Q3 | только `city_id` | мгновенно | <1 мс* | обход `listings_created_at_idx` с фильтром |
| Q4 | `rooms + area_m2 BETWEEN` | мгновенно | 0.3 мс | обход `listings_created_at_idx` (+ BitmapAnd с `listings_rooms_idx` при полном фильтре Q6) |
| Q5 | `deal_type` + сортировка `price_usd ASC, id` | Seq Scan + top-N | Parallel Seq Scan | — (см. п. «Наблюдение 2») |
| Q6 | полный фильтр (страна, город, тип, комнаты, площадь, цена) | мгновенно | мгновенно | BitmapAnd `deal_city_price` + `rooms` |
| Q7 | поиск пар-дублей по `content_hash` | Seq Scan (41 строка — планнер прав) | → индекс `listing_duplicates_content_hash_idx` | |
| Q8 | опрос outbox `WHERE published_at IS NULL ORDER BY id LIMIT 10` + `FOR UPDATE SKIP LOCKED` | мгновенно | мгновенно | `outbox_events_pkey` при 33% pending; частичный `outbox_events_pending_idx` включается, когда доля pending мала |
| Q9 | глубокая страница (`OFFSET 540`) | мгновенно | — | `listings_created_at_idx` |

\* при плотном результате; худший случай ниже.

## Худший случай (главная находка)

Фильтр, дающий **пустой результат**, с сортировкой по дате: «rent в Brest
дороже 900 USD» на стресс-объёме:

- через обход `listings_created_at_idx`: планировщик читает весь индекс
  (**54 000 строк отфильтровано**) — **17.7 мс**;
- тот же предикат без сортировки (COUNT) — Bitmap Index Scan по
  `listings_deal_city_price_idx` — **0.045 мс** (×400 быстрее).

17.7 мс всё равно на два порядка лучше бюджета НФТ-10 (<500 мс без кэша),
поэтому **изменений схемы не требуется**. Но этот кейс объясняет, почему
фильтры «без типового `deal_type`» (Q3, Q4) — кандидаты на наблюдение.

## Наблюдения (решить после реальных запросов, не менять молча)

1. **City-only / rooms-only фильтры** (Q3, Q4): сейчас работают через обход
   `created_at` с фильтром — быстро при плотном результате, до полного
   обхода индекса при почти пустом. Если в реальных запросах такие фильтры
   часты — кандидат в миграцию: индекс `(city_id, created_at DESC)`.
2. **Сортировка только по цене** (Q5): `price_usd` не ведущая колонка ни
   одного индекса, поэтому при 54k планировщик уходит в Seq Scan. На
   демо-объёме несущественно; если p95 по «price_asc без города» станет
   узким местом — кандидат `(deal_type, price_usd, id)`.

Оба пункта закроются замерами из `scripts/bench/coreapi-p95.sh` (PR #76)
на реальном коде Core API — если бюджет и так выполняется, схема остаётся
как есть.

## Как воспроизвести

```bash
docker compose up -d postgres        # или одноразовый контейнер на свободном порту
cd core-api && alembic upgrade head  # схема из PR #81
# данные (из корня; DATABASE_URL — как в .env.example)
DATABASE_URL=postgresql+asyncpg://app:app@127.0.0.1:5432/realestate \
  .venv/Scripts/python -I scripts/db/gen-analysis-data.py --truncate --count 520
# стресс-прогон
DATABASE_URL=... .venv/Scripts/python -I scripts/db/gen-analysis-data.py --truncate --count 50000 --seed 777
# планы — см. таблицу выше, например:
# EXPLAIN (ANALYZE, TIMING OFF) SELECT ... WHERE deal_type='rent' AND city_id=1 ...
```

## Схема notifier — анализ запросов (дополнение)

Отдельный прогон по второй схеме (ТЗ 6.1): запросы взяты из кода notifier
в main (`internal/store/postgres.go`, `internal/notify/journal_pg.go`,
`limiter_pg.go`, `queue_pg.go`), данные — `scripts/db/gen-notifier-data.py`:
5 000 пользователей, 24 850 подписок, 150 000 строк журнала за 30 дней
(sent 75% / unknown 5% / failed 5% / deferred 15%), очередь — 22 439
заданий. Масштаб ×5–×10 от демо-нагрузки ТЗ.

| # | Запрос (источник) | Время | Индекс |
|---|---|---|---|
| N1 | Диспетчер: `SELECT ... FROM subscriptions ORDER BY id` (dispatch All) | 2.9 мс | `subscriptions_pkey`, полный обход |
| N2 | Подписки чата: `WHERE chat_id = $1` (ByChat) | <0.1 мс | `subscriptions_chat_id_idx` |
| N3 | Журнал «уже доставлено?»: `EXISTS ... WHERE chat_id AND listing_id` (journal Sent) | 0.03 мс | `notifications_log_user_listing_uq` |
| N4 | Лимитер 30/час: `count ... WHERE chat_id AND mode AND sent_at > now()-1h` | 0.08 мс | `notifications_log_rate_idx` |
| N5 | Pop очереди, **код до PR #72**: `WHERE status='deferred' ORDER BY created_at LIMIT 10 FOR UPDATE SKIP LOCKED` | **44.3 мс** | см. «PopBatch» ниже |
| N6 | Pop очереди, **целевой вид из 0002, в main с PR #72**: `WHERE status='deferred' AND available_at <= now() ORDER BY available_at` | 0.045 мс | `notifications_log_available_idx` |
| N7 | Mark: `UPDATE ... WHERE chat_id AND listing_id AND status='deferred'` | <0.1 мс | `notifications_log_user_listing_uq` |
| N8 | `/stop`: `DELETE FROM users` с каскадом подписок и журнала | 0.27 мс | PK + каскадные FK |

### ПопBatch: была находка — уже внедрена (PR #72)

История: PopBatch в коде на момент анализа использовал
`WHERE status = 'deferred' ORDER BY created_at`, а миграция 0002 к этому
моменту уже заменила индекс deferred-очереди на `available_at`
(`notifications_log_available_idx`). Разрыв форм запросов на бэклоге
22 439 deferred давал: планировщик собирал **все** deferred-строки
bitmap-сканом и сортировал **на диске** (external merge, 8.8 МБ) ради
LIMIT 10 — **44.3 мс** на каждый проход флешера (раз в 30 с, 8 воркеров),
под `FOR UPDATE`-блокировками. Целевой вид из комментария миграции —
прямо по индексу: **0.045 мс** (**×1000**).

Даниил реализовал целевой вид в PR #72 (`WHERE status = 'deferred'
AND available_at <= now() ORDER BY available_at, id`) — в main PopBatch
идёт по индексу, backoff-семантика `available_at` включена. Замер выше
сохранён как обоснование этого перехода.

### Наблюдение: диспетчер читает все подписки на каждое событие

`MatchAll` выполняет `SELECT ... FROM subscriptions` целиком (2.9 мс на
24 850 строк) и фильтрует в Go. На демо-нагрузке ок; если подписок станет
на порядки больше — кандидат на SQL-матчинг или инкрементальную выборку
по индексам подписок (страна+город). Решение за Даниилом, схема не мешает.

### Наблюдение: available_at

На момент анализа `available_at` (миграция 0002) в коде notifier не
использовался — PopBatch работал через `created_at` (совместимо, как и
заявлено в миграции). С мержем PR #72 колонка задействована: backoff
повтора и выборка очереди идут по `available_at`.

### Как воспроизвести

```bash
docker run -d --name pg-n -e POSTGRES_USER=app -e POSTGRES_PASSWORD=app \
  -e POSTGRES_DB=realestate -p 127.0.0.1:5448:5432 postgres:16
for f in notifier/migrations/*.sql; do docker exec -i pg-n psql -U app \
  -d realestate -v ON_ERROR_STOP=1 -q < "$f"; done
DATABASE_URL=postgresql://app:app@127.0.0.1:5448/realestate \
  .venv/Scripts/python -I scripts/db/gen-notifier-data.py --truncate
# планы — запросы N1–N8 из таблицы выше
```
