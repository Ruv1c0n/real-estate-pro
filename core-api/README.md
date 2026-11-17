# core-api

Python/FastAPI-сервис: объявления, поиск, справочники, кэш, transactional outbox, публикация событий в Redis Streams. Порт **8000**. Зона ответственности — Тимур.

Реализует ФТ-2.1–2.7 и НФТ-6/12. Контракты — [docs/contracts.md](../docs/contracts.md).

## Что есть сейчас

| Компонент (`app/`) | Статус |
|---|---|
| `main.py` | FastAPI-приложение: lifespan (движок БД + Redis), логирование, `/` |
| `health.py`, `health_models.py` | `GET /health` (200 ready / 200 degraded / 503), `/health/live`, `/health/ready` — семантика ФТ-2.7 |
| `config.py` | `DATABASE_URL` (приоритет) или `POSTGRES_*` части с %-экранированием (quote); `REDIS_URL`; `LOG_LEVEL` |
| `error_handlers.py` | Ошибки API в формате `{error, message}`, включая 422 (НФТ-12) |
| `listing_filters.py` | Валидация фильтров `GET /api/v1/listings` (deal_type, страна/город, цена, комнаты, площадь, source, sort, page/size) — модель готова, **эндпоинт ещё не подключён** |
| `redis_client.py`, `database.py`, `logging_setup.py` | Клиенты Redis/SQLAlchemy, настройка логов |

OpenAPI автоматически: `http://localhost:8000/docs`.

## Планируется (набросок, по ТЗ и плану Тимура)

- `POST /api/v1/listings` — 201 `{id, created:true}` / 200 `{id, created:false}`; дубль по `(source, external_id)` без нового события; UNIQUE на стороне БД.
- `GET /api/v1/listings`, `GET /api/v1/listings/{id}` — поиск/сортировка/пагинация за подключённым `ListingFilters`.
- Справочники `countries/cities/sources/currencies` (notifier уже ждёт их через readiness).
- Transactional outbox: сохранение объявления + задания одной транзакцией, фоновый `XADD listings.new` (контракт — contracts.md §2).
- Кэш списков: TTL 600 с, ключ с учётом параметров и `cache:version`, инвалидация фоновой задачей.
- Alembic-миграции схемы `core` — дизайн в [docs/core-schema-draft.md](../docs/core-schema-draft.md), реализация идёт в PR #81.

## Переменные окружения

| Переменная | Назначение |
|---|---|
| `DATABASE_URL` | DSN целиком (`postgresql+asyncpg://...`); имеет приоритет над `POSTGRES_*` |
| `POSTGRES_HOST/PORT/USER/PASSWORD/DB` | Адрес по частям; значения %-экранируются |
| `REDIS_URL` | Адрес Redis (дефолт `redis://localhost:6379/0`) |
| `LOG_LEVEL` | `DEBUG`…`CRITICAL`, дефолт `INFO` |

## Запуск

```bash
# в составе стенда (из корня репо)
docker compose up --build

# локально
cd core-api
pip install -r requirements.txt
uvicorn app.main:app --port 8000
```

## Тесты

```bash
cd core-api && pytest
```

CI: `python-ci` (ruff + pytest), авто-job Alembic в `migrations` активируется при появлении `alembic.ini`.
