## Запуск
docker compose up --build

## Сервисы
- core-api (FastAPI) — :8000 — Илья, папка core-api/
- collector (парсер + UI) — :8001 — Дима, папка collector/
- notifier (Go) — :8080 — папка notifier/
- postgres :5432, redis :6379