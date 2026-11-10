# Contributing

[English](CONTRIBUTING.md) | [Русский](CONTRIBUTING.ru.md)

Учебный командный проект. Правила ниже обязательны для всех участников.

## Зоны ответственности

| Кто     | Зона                                        |
|---------|---------------------------------------------|
| Даниил  | notifier (Go), Telegram-бот, docker-compose |
| Илья    | PostgreSQL, миграции (Alembic `core`, SQL `notifier`) |
| Дима    | UI, mock-данные, ручные проверки            |
| Тимур   | core-api, collector (Python)                |

Чужую зону не трогаем: изменения в ней — только через согласие владельца (ревью в PR или обсуждение в issue).

## Ветки и коммиты

- Работа — в feature-ветке, затем PR в `main`. Коммитить напрямую в `main` нельзя.
- Стиль коммитов: `feat(notifier): ...`, `fix(core-api): ...`, `ci: ...`, `docs: ...`.
- PR мержится **merge-коммитом** (не squash) — решение команды от 29.10.2026.
- Перед PR: ветка должна быть up-to-date с `main` — ruleset `require-ci` это проверяет.

## Что нельзя менять без согласования команды

Требования ТЗ, раздел 6.5:

- контракты из `docs/contracts.md`: событие `listings.new`, `POST /api/v1/listings`, формат `RawListing`;
- схема БД меняется **только миграциями**:
  - схема `core` — Alembic;
  - схема `notifier` — SQL-миграции (автор — Илья, применяет Даниил):
    файлы `notifier/migrations/*.sql` встраиваются в бинарник и применяются
    при старте notifier автоматически; имя файла — `000N_описание.sql`,
    порядок лексикографический; каждый файл сам открывает и фиксирует
    транзакцию (`BEGIN`/`COMMIT`) — раннер выполняет файл одним `Exec`;
- каждый сервис работает только со своей схемой Postgres; чужие данные — через API или события;
- секреты — только через `.env` (в репо есть `.env.example`; CI проверяет утечки через gitleaks).

План изменения контракта сначала обсуждается в issue, потом код.

## CI

PR в `main` проходит `ci` (также запускается на push в `main`):

- `notifier`: gofmt, `go vet`, `go test -race`;
- `gitleaks`: поиск случайных секретов;
- `docker`: `docker compose config` + `docker compose build`.

Эти три проверки обязательны для merge (ruleset `require-ci`).

`python-ci` (ruff и тесты) запускается при изменениях в `core-api/`, `collector/`, `requirements-lint.txt` или самом workflow — на push в любую ветку и на PR в `main`. Он не входит в обязательные проверки, но зелёный PR — правило хорошего тона.

Merge также требует одобрения владельца затронутой зоны.

## Локальный запуск

```bash
cp .env.example .env   # заполнить секреты
docker compose up --build
```

Проверки перед коммитом (notifier):

```bash
cd notifier && gofmt -l . && go vet ./... && go test ./...
```

### Тесты с PostgreSQL и Redis

Часть тестов notifier работает против PostgreSQL и Redis. В CI сервисов
postgres/redis нет, поэтому эти тесты там скипаются — это норма. Локальный запуск:

```bash
docker compose up -d postgres redis
# один раз: выделенная тестовая БД (реквизиты по умолчанию app/app;
# скорректируйте, если меняли POSTGRES_USER/POSTGRES_PASSWORD в .env)
docker compose exec postgres psql -U app -d realestate -c "CREATE DATABASE realestate_test;"
cd notifier
NOTIFIER_TEST_DSN='host=127.0.0.1 user=app password=app dbname=realestate_test' go test ./...
```
