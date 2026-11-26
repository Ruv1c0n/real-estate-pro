# docs

Документация проекта. Приоритет источников: **contracts.md → README → этот каталог**.

| Файл | Что это |
|---|---|
| [Real_Estate_Pro_TZ.pdf](Real_Estate_Pro_TZ.pdf) | Техническое задание, 9 стр. Требования пронумерованы: ФТ-x.x — функциональные, НФТ-x — нефункциональные. Раздел 6.5 — правила изменений (контракты, миграции, границы зон). |
| [contracts.md](contracts.md) | Актуальные контракты между сервисами: `POST /api/v1/listings`, событие `listings.new` (Redis Streams), справочники, mock-источник. При расхождении с README приоритет здесь. |
| [core-schema-draft.md](core-schema-draft.md) | Дизайн-док схемы `core` (согласована, смержена в PR #66). Alembic-миграция влита (PR #81): источник правды по схеме — миграция `core-api/alembic/`, этот документ остаётся историческим обоснованием решений. |
| [db-backup-restore.md](db-backup-restore.md) | Регламент бэкапа/восстановления PostgreSQL: «дамп без успешной restore-verify бэкапом не считается» (PR #83). |
| [db-quickstart-for-devs.md](db-quickstart-for-devs.md) | Быстрый старт разработчика с PostgreSQL (PR #83). |
| [er-diagram.md](er-diagram.md) | ER-диаграмма схемы `core` (ТЗ 6.1, PR #86): сущности и связи по Alembic-миграциям. |
| [er-diagram-notifier.md](er-diagram-notifier.md) | ER-диаграмма схемы `notifier` (ТЗ 6.1, issue #120): сущности и связи по SQL-миграциям notifier. |
| [incident-runbook.md](incident-runbook.md) | Runbook инцидентов: симптом → диагноз → действия → «починилось, если». Корреляция по `event_id`/`X-Request-ID`. |

## Правила изменений (ТЗ 6.5, кратко)

- Контракты (`listings.new`, `POST /api/v1/listings`, `RawListing`) — только по согласованию команды: сначала план, потом код.
- Схема БД — только миграции: Alembic для `core`, SQL для `notifier`.
- Каждый сервис работает только со своей схемой PostgreSQL; чужие данные — через API/события.
- Секреты — только через `.env`; в репозитории их быть не должно.
