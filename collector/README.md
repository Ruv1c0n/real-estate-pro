# collector

Python/httpx/Jinja2/HTMX-сервис: API-адаптер источника, нормализация, доставка в Core API, веб-страницы. Порт **8001**. Зона ответственности — Дима (UI, mock-данные), Тимур (адаптер/нормализация).

Реализует ФТ-1.1–1.8 и НФТ-15.

## Что есть сейчас

Заглушка: `Dockerfile` поднимает `python -m http.server 8001`. Приложение не реализовано; `MOCK_MODE=true` в Compose захардкожен, но Python-логика ещё не читает его (честно отражено в корневом README).

## Планируется (набросок, по ТЗ)

- **Адаптеры** — `AvitoApiAdapter` (по документированному методу API Авито; без реквизитов проверяется на фикстурах) и `MockAdapter` (читает внутренний `RawListing` из дампа ≥500 объявлений / 5 стран / 10 городов).
- **Режимы mock** — `batch` (дамп целиком, вручную или по расписанию) и `stream` (объект раз в 5–30 с; `MOCK_PUBLISH_MODE`). Стабильные `external_id` при повторных отправках.
- **Нормализация** — приведение городов к справочнику, `price_usd` (курс Core API, Decimal + `ROUND_HALF_UP`), очистка HTML, валидация комнат/площади.
- **Доставка** — `POST /api/v1/listings`, до 5 повторов с backoff 1–30 с и учётом `Retry-After`; Redis-дедуп до отправки, хэш-флаг после 200/201.
- **Расписание** — сбор каждые 15 минут; `POST /collect/run` (202 с `run_id`, 409 если уже идёт) и `GET /collect/runs/{run_id}` под `X-Admin-Key`.
- **UI (НФТ-15)** — две страницы Jinja2+HTMX: список объявлений из Core API и ошибки/пустые результаты; данные только через Core API, отметка `source=mock`.

Формат `RawListing` — [docs/contracts.md](../docs/contracts.md) §3; mock-источник (ТЗ 4.6) — отдельный сервер mock-avito (PR #62/#63), collector остаётся без парсинга HTML.

## Запуск

```bash
# в составе стенда (из корня репо)
docker compose up --build

# локально — пока только заглушка
cd collector && python -m http.server 8001
```

## Статус

Не начат (UI — ветка `feat/collector-ui` без PR). Разблокируется справочниками и `POST /api/v1/listings` в core-api (план: POST → GET → outbox → Alembic → адаптер).
