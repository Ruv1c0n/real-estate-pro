# .github

CI/CD репозитория: GitHub Actions, гварды ссылок, шаблоны. Все workflow запускаются на PR в `main` и на push в `main`.

## Workflows ([workflows/](workflows/))

| Workflow | Что проверяет |
|---|---|
| [ci.yml](workflows/ci.yml) | notifier: `gofmt`, `go vet`, `go test` + джоба `notifier-pg-tests` (postgres/redis services, интеграционные Pg-тесты с `-race -p 1`); сборка docker-образов; gitleaks |
| [python-ci.yml](workflows/python-ci.yml) | `ruff` и `pytest` для core-api и collector (ruff гоняется из корня — конфиг один на репо) |
| [migrations.yml](workflows/migrations.yml) | Накат SQL-миграций notifier с нуля на чистой PostgreSQL; авто-job Alembic для схемы `core` активируется сама при появлении `alembic.ini` |
| [docs-links.yml](docs-links.yml) | Все относительные ссылки в markdown ведут на существующие файлы (якоря `#section` сознательно не проверяются) |
| [graphify-pages.yml](graphify-pages.yml) | Пересборка графа knowledge-graph и публикация GitHub Pages |

## Скрипты и конфиги

| Файл | Назначение |
|---|---|
| [scripts/check-md-links.sh](scripts/check-md-links.sh) | Локальный запуск той же проверки ссылок, что в docs-links: `bash .github/scripts/check-md-links.sh` |
| [CODEOWNERS](CODEOWNERS) | Автоназначение ревьюеров по зонам репо |
| [dependabot.yml](dependabot.yml) | Обновления github-actions (одной группой) и go-модулей notifier |
| [PULL_REQUEST_TEMPLATE.md](PULL_REQUEST_TEMPLATE.md) | Чеклист PR: связка с ТЗ (ФТ/НФТ), контракты, миграции, секреты — повторяет правила раздела 6.5 |

## Заметки

- Правила веток/коммитов и процесс PR — [CONTRIBUTING.md](../CONTRIBUTING.md).
- Имена бейджей в README соответствуют именам workflow-файлов: `ci`, `python-ci`, `migrations`, `docs-links`.
