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
| `POST /debug/event` | Демо-событие в `listings.new`; заголовок `X-Admin-Key` (без ключа — 404) |

## Переменные окружения

| Переменная | Назначение |
|---|---|
| `TELEGRAM_BOT_TOKEN` | Без токена — режим `log` (совпадения в stdout) |
| `REDIS_URL` | Адрес Redis (в Compose `redis://redis:6379/0`) |
| `POSTGRES_HOST/PORT/USER/PASSWORD/DB` | Адрес PostgreSQL по частям; не заданы всё — memory-режим (лог `DB disabled: running in memory mode`) |
| `POSTGRES_SSLMODE` | `sslmode` в собираемом DSN; пусто = `disable` |
| `DATABASE_URL` | Legacy-вариант целиком (проверяется первым; только форматы, парсируемые pgx) |
| `ADMIN_KEY` | Ключ `/debug/event`; пусто — эндпоинт отвечает 404 |

## Запуск

```bash
# в составе стенда (из корня репо)
docker compose up --build

# локально
cd notifier && go run .
```

БД недоступна на старте при заданных `POSTGRES_*` — сервис завершается с ошибкой (fail-fast). Memory-режим: подписки живут до рестарта.

## Миграции

SQL-миграции схемы `notifier` — [migrations/](migrations/) (`0001_init.sql`, `0002_available_at.sql`). Готовит Илья; применение при деплое — зона Даниила. `embed.go` вшивает каталог в бинарник.

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

По коду ТЗ — 100%; надёжностные хвосты — в открытых PR (`#68` условный XACK, `#70` UserExists, `#72` available_at, `#73` очистка stream/env-параметры): см. раздел «Target configuration» корневого README.
