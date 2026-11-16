# Резервное копирование и проверка восстановления

Отвечает: Илья (ТЗ v1.1, раздел 6.1: «резервное копирование и проверка восстановления»).
Скрипты: [scripts/db/backup.sh](../scripts/db/backup.sh),
[scripts/db/restore-verify.sh](../scripts/db/restore-verify.sh).

## Что покрывается

- PostgreSQL: обе схемы (`core`, `notifier`), таблица версий `alembic_version` (public) —
  всё, что нужно для полного восстановления структуры и данных. Дамп в custom-формате
  `pg_dump -Fc` (сжатый, восстанавливается `pg_restore`).
- Не покрывается и не требует: Redis — кэш `dedup:urls` и `cache:version`
  восстанавливаются/очищаются без потери данных (ТЗ 3.3, 3.5), события
  `listings.new` переживают рестарт notifier'а благодаря AOF (compose, ТЗ 4.3).

## Дамп

```bash
# из корня репо; источник — сервис postgres из docker-compose (должен быть запущен)
scripts/db/backup.sh
```

Создаёт пару файлов в `backups/` (каталог в `.gitignore`):

| файл | содержимое |
|---|---|
| `realestate-YYYYmmdd-HHMMSS.dump` | дамп `-Fc` |
| `realestate-YYYYmmdd-HHMMSS.dump.manifest.tsv` | `schema.table\|rows` на момент дампа — эталон для проверки |

Путь дампа можно задать первым аргументом. Для источника вне compose:

```bash
PG_MODE=host PG_HOST=host.docker.internal PG_PORT=5433 scripts/db/backup.sh
```

Учётные данные берутся из окружения (`PG_USER`, `PG_PASSWORD`, `PG_DB`),
по умолчанию совпадают с `.env.example`; пароль только в переменной окружения
разового контейнера, в файлы не попадает.

## Проверка восстановления (обязательно для каждого дампа)

```bash
scripts/db/restore-verify.sh backups/realestate-YYYYmmdd-HHMMSS.dump
```

Скрипт поднимает одноразовый контейнер `postgres:16` на `127.0.0.1:55433`,
восстанавливает дамп `pg_restore` и сверяет число строк в каждой таблице
с манифестом. `PASS` — дамп полный; `FAIL` с diff'ом — дамп битый или
устарел. Контейнер удаляется в любом случае.

Регламент команды: дамп без успешной `restore-verify` бэкапом не считается.

## Расписание и хранение

- Перед демонстрацией и перед опасными операциями (миграции, `down -v`) —
  обязательный дамп + проверка.
- Ежедневный дамп по расписанию (пример, Git Bash на хосте с Docker):

  ```bash
  0 4 * * * cd /path/to/repo && scripts/db/backup.sh >> backups/backup.log 2>&1
  ```

  В Windows Task Scheduler — тот же скрипт через `bash -lc`.
- Хранение: держать последние 7 ежедневных + 1 еженедельный; локальные дампы
  не в репозитории (`.gitignore`). Дампы могут содержать пользовательские
  данные — не выкладывать их наружу.
- Восстановление на живой стек — вручную, со остановленными сервисами:

  ```bash
  docker compose stop core-api collector notifier
  # структура и данные из дампа (--clean делает DROP ... CASCADE,
  # в том числе схем — с ними теряются и GRANT'ы, см. шаг 2)
  pg_restore -h 127.0.0.1 -p 5432 -U app -d realestate \
      --clean --if-exists --no-owner --no-privileges <dump>
  # шаг 2: переустановка ролей и прав кодом миграции 0002 —
  # после DROP ... CASCADE GRANT'ы и default privileges на схемах
  # потеряны, сервисы без этого не заработают
  cd core-api && alembic downgrade 0001 && alembic upgrade head && cd ..
  # шаг 3: выдать пароли ролей (значения из .env, как при депое)
  # шаг 4: запуск сервисов
  docker compose start core-api collector notifier
  ```

- Полная потеря volume (свежая база): тот же порядок, что и на живом стеке,
  но сначала `alembic upgrade head` на пустой БД (структура + роли),
  затем `pg_restore --clean --if-exists --no-owner --no-privileges`,
  затем `alembic downgrade 0001 && alembic upgrade head` и пароли ролей.
  Проверено на postgres:16: после цикла данные целы, `core_api`/`notifier`
  имеют все права, default privileges на месте. Runner notifier свои
  SQL-миграции повторно применять не будет, если `notifier.schema_migrations`
  приехал в дампе; если дамп старше запуска runner'а — миграции применятся
  заново и завершатся no-op'ом благодаря IF NOT EXISTS.
