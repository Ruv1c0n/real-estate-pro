# notifier

Go-сервис уведомлений: Telegram-бот, consumer Redis Streams `listings.new`, журнал отправок, лимит 30/час, отложенная очередь. Порт **8080**. Зона ответственности — Даниил.

Реализует ФТ-3.1–3.7 и ТЗ 4.2/4.3. Корневой [README](../README.md) описывает поток данных; контракты — [docs/contracts.md](../docs/contracts.md).

## Компоненты (`internal/`)

| Пакет | Назначение |
|---|---|
| `bot` | Telegram-команды `/start`, `/subscribe`, `/list`, `/unsubscribe <id>`, `/stop`; валидация подписок по справочникам (с фолбэком на stale-кэш) |
| `store` | Пользователи и подписки: `Postgres` (схема `notifier`) или `Memory` (fallback без БД) |
| `db` | Пул pgx, DSN из `POSTGRES_*` частей (keyword-value, quote-безопасный) или legacy `DATABASE_URL` |
| `migrate` | Встроенный раннер: применяет `migrations/*.sql` лексикографически при старте |
| `notify` | Journal (журнал, UNIQUE user+listing), Limiter (окно 30/час), Queue (отложенные, backoff по `available_at`), Dispatcher |
| `stream` | Consumer group `notifier` с позиции 0, `XREADGROUP`, `XAUTOCLAIM` (pending > 30 с), 8 воркеров |
| `dicts` | Клиент справочников Core API + кэш |
| `telegram` | Отправка через Bot API; без токена — режим `log` (stdout) |
| `model` | `RawListing`-событие (`ParseEvent`/`Validate`), подписки |

## HTTP

| Метод и путь | Назначение |
|---|---|
| `GET /health` | Liveness: `db`, `redis`, `dicts`, режим доставки |
| `GET /health/ready` | Readiness: 200 только при доступных Redis, справочниках и (в db-режиме) PostgreSQL; иначе 503 |
| `GET /metrics` | Метрики Prometheus (только серии `notifier_*`) — см. «Метрики» |
| `POST /debug/event` | Демо-событие в `listings.new`; заголовок `X-Admin-Key` (без ключа — 404) |
| заголовок `X-Request-ID` | На любой путь: эхо входящего или сгенерированный 16-hex id; возвращается в ответе и попадает в лог запроса (вход длиннее 64 символов или с непечатаемыми ASCII/кавычками заменяется сгенерированным) |

## Метрики

`GET /metrics` отдаёт Prometheus-формат из кастомного реестра — только серии `notifier_*` (runtime-коллекторы Go `go_*`/`process_*` не экспонируются).

Бизнес-метрики:

| Метрика | Что значит |
|---|---|
| `notifier_stream_pending_events` | Записи в pending consumer group (залипшие воркеры) |
| `notifier_stream_length` | Длина стрима `listings.new` (XLEN); парный к мягкому порогу WARN (ТЗ 3.6) |
| `notifier_deferred_jobs` | Отложенные задания: строки `deferred` в `notifications_log` (лимит 30/час) |
| `notifier_quarantined_jobs` | Карантин: строки `failed` в `notifications_log` (нечитаемый payload — пара заблокирована навсегда) |
| `notifier_delivery_duration_seconds` | Длительность отправки в Telegram; лейбл `outcome` = `sent`/`rate_limited`/`unknown` (p95 НФТ-11 считать по `sent`) |
| `notifier_telegram_retry_after_total` | Ответы Telegram 429 Retry-After |
| `notifier_dead_letter_total` | Записи, уложенные в `listings.dead` с причиной (битые payload) |
| `notifier_event_process_errors_total` | Обработка события упала на временной ошибке — запись осталась в pending, вернётся по XAUTOCLAIM |
| `notifier_gauge_poll_errors_total` | Неудачные проходы поллера гейджей (Redis/PG недоступны — гейджи замерли на прошлом значении) |

HTTP-метрики (`{code, method}` у первых двух):

| Метрика | Что значит |
|---|---|
| `notifier_http_requests_total` | HTTP-запросы по коду и методу |
| `notifier_http_request_duration_seconds` | Длительность обработки HTTP-запроса |
| `notifier_http_requests_in_flight` | Запросы в обработке прямо сейчас |

Гейджи обновляет фоновый поллер раз в 15 с — мгновенный scrape показывает значения прошлого прохода. В memory-режиме `notifier_deferred_jobs` и `notifier_quarantined_jobs` не экспонируются (БД нет — «0» выглядел бы как «очередь пуста»).

Экспозиция: `/metrics` отдаётся без ключа на том же порту 8080 — значений секретов и PII не содержит; при выносе сервиса из внутренней сети прикрыть ключом или выделить отдельный порт.

## Переменные окружения

| Переменная | Назначение |
|---|---|
| `TELEGRAM_BOT_TOKEN` | Без токена — режим `log` (совпадения в stdout) |
| `REDIS_URL` | Адрес Redis (в Compose `redis://redis:6379/0`) |
| `POSTGRES_HOST/PORT/USER/PASSWORD/DB` | Адрес PostgreSQL по частям; не заданы всё — memory-режим (лог `DB disabled: running in memory mode`) |
| `POSTGRES_SSLMODE` | `sslmode` в собираемом DSN; пусто = `disable` |
| `DATABASE_URL` | Legacy-вариант целиком (проверяется первым; только форматы, парсируемые pgx) |
| `ADMIN_KEY` | Ключ `/debug/event`; пусто — эндпоинт отвечает 404 |
| `NOTIFIER_LOG_LEVEL` | Уровень лога: `DEBUG`/`INFO`/`WARN`/`ERROR` (регистр не важен, дефолт `INFO`); мусорное значение — fail-fast при старте |

## Логи

JSON-структура в stdout (`log/slog`, карточка плана prod-10of10); уровень — `NOTIFIER_LOG_LEVEL`. Сквозная корреляция — по `event_id` (контракт `listings.new`), она проходит consumer → dispatch → flush; HTTP-запросы коррелируются по `request_id`.

- `event_id` — событие из стрима; `chat_id`, `listing_id` — адресат и объявление; `stream_msg_id` — запись в `listings.new`;
- `request_id`, `client_ip` — HTTP-запрос и его клиент; `status`, `duration_ms` — код и длительность; `error` — ошибка;
- `/health*` и `/metrics` (healthcheck compose, скрейп) пишутся на `DEBUG` — INFO не засоряется регулярным шумом;
- входящий `X-Request-ID` принимается только печатным ASCII без кавычек до 64 символов, иначе заменяется сгенерированным.

## Запуск

```bash
# в составе стенда (из корня репо)
docker compose up --build

# локально
cd notifier && go run .
```

БД недоступна на старте при заданных `POSTGRES_*` — сервис завершается с ошибкой (fail-fast). Memory-режим: подписки живут до рестарта.

## Миграции

SQL-миграции схемы `notifier` — [migrations/](migrations/) (`0001_init.sql`, `0002_available_at.sql`, `0003_reason.sql`). Готовит Илья; применение при деплое — зона Даниила. `embed.go` вшивает каталог в бинарник.

## Тесты

```bash
cd notifier && go vet ./... && go test ./...
# с живыми Postgres/Redis (джоба notifier-pg-tests в CI делает то же):
docker compose up -d postgres redis
NOTIFIER_TEST_DSN='host=127.0.0.1 port=5432 user=app password=app dbname=realestate_test' \
NOTIFIER_TEST_REDIS_ADDR='127.0.0.1:6379' go test -race -count=1 -p 1 ./...
```

Подробности и создание тестовой БД — [CONTRIBUTING.md](../CONTRIBUTING.md#postgresqlredis-tests).

## Статус

По коду ТЗ — 100%; надёжностные хвосты — в открытых PR (`#68` условный XACK, `#69` Pg-тесты в CI, `#70` UserExists, `#71` infra-гигиена, `#72` available_at, `#73` очистка stream/env-параметры): см. раздел «Target configuration» корневого README.
