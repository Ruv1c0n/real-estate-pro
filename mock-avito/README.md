# mock-avito

Имитация внешнего источника объявлений (примерно как Avito) для разработки и
демонстрации Collector'а без реального сайта. Контракт — `docs/contracts.md`,
раздел 4. Стек: Python/FastAPI/uvicorn/Jinja2/HTMX, состояние в памяти,
без БД; рестарт пересоздаёт набор из фиксированного seed.

## Запуск

```bash
# только mock-avito (профиль mock — обычный docker compose up его не поднимает)
docker compose --profile mock up -d --build mock-avito

# остановить
docker compose --profile mock down
```

Открыть консоль управления: <http://127.0.0.1:8002/admin/ui>

## Ручки источника (их использует Collector)

| Метод | Ответ |
|---|---|
| `GET /search/listings` | `{items, total, page, size}`; `page` ≥1, `size` 1–100; фильтры `deal_type`, `country`, `city`, `price_min`/`price_max`, `rooms`; сортировка `published_at` desc |
| `GET /item/{id}` | Карточка; неизвестный ID — 404 |
| `GET /health` | `{"status": "ok", "mode": ..., "total": ..., ...}` |

Хаос для проверки ретраев Collector'а (ФТ-1.6): `?fail=429` / `?fail=500`
с `?fail_times=N` — N подряд запросов возвращают код (для 429 — с
`Retry-After`), затем успешный ответ.

## Данные

520 объявлений, 5 стран (BY/RU/PL/UA/KZ), 13 городов — набор самостоятельный;
при появлении справочника Core API синхронизировать с ним `app/generator.py`
(сверка набора — задача интеграции). Кириллица в `title`/`address`/`description` — живой тест
кодировок; часть описаний содержит HTML — для проверки очистки у адаптера.
`images` — ссылки на заглушки `/img/…` (SVG), сами картинки не имитируются.

- **batch** — полный набор с постоянными ID (`seed-000001`…);
- **stream** — новые объявления с уникальными ID (`gen-000001`…) каждые 5–30 с.

## Управление (X-Admin-Key)

Ключ задаётся `MOCK_ADMIN_KEY`; пустое значение выключает управление.
Без ключа ручки управления отвечают 404, с неверным ключом — 403.
Консоль `/admin/ui` и её фрагменты статуса/ленты (`/admin/ui/state`,
`/admin/ui/feed`) открыты всегда — только чтение, действия требуют ключа.

| Метод | Тело | Действие |
|---|---|---|
| `GET /admin/state` | — | текущее состояние и метрики |
| `POST /admin/mode` | `{"mode": "batch"\|"stream"}` | переключение режима без рестарта |
| `POST /admin/chaos` | `{"code": 0\|429\|500, "every": N}` | глобальный хаос: каждый N-й запрос к данным → код (0 = выкл) |
| `POST /admin/delay` | `{"ms": 0..5000}` | искусственная задержка ответов на `/search/listings` и `/item` (проверка таймаутов Collector) |
| `POST /admin/rate-limit` | `{"per_minute": N}` | глобальный лимит запросов (0 = выкл), превышение → 429 + `Retry-After` |
| `POST /admin/bad-data` | `{"enabled": bool, "rate": 0..1}` | ломаные объявления (цена 0, неизвестный город, `deal_type` вне rent/sale, комнаты <0) — проверка 422-ветки Collector → Core API (ТЗ 3.1) |
| `POST /admin/reset` | `{"seed": int?}` | пересоздать набор; если bad data включены — подмешать долю |

Консоль `/admin/ui`: статус (обновление 2 с), кнопки управления,
лента последних публикаций. Вводится ключ доступа.

## Переменные окружения

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `MOCK_PUBLISH_MODE` | `batch` | `batch` / `stream` |
| `MOCK_SEED` | `42` | seed генератора |
| `MOCK_COUNT` | `520` | размер базового набора |
| `MOCK_ADMIN_KEY` | пусто (выкл) | ключ управления |
| `MOCK_STREAM_MIN_SEC` / `MOCK_STREAM_MAX_SEC` | `5` / `30` | интервал публикации (для тестов) |
| `MOCK_STREAM_MAX_ITEMS` | `5000` | предел потока, дальше вытесняются старейшие |

## Тесты

```bash
cd mock-avito
python -m venv .venv && .venv/Scripts/pip install -r requirements.txt -r requirements-dev.txt
.venv/Scripts/python -m pytest tests/ -q
```
