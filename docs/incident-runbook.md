# Runbook инцидентов — симптомы → диагноз → действия

> Операционный документ (карточка 09 плана prod-10of10). Цель: по симптому
> в логах/метриках добраться до причины и понять, «починилось ли» — без
> чтения исходников на 3 часа ночи.
>
> **Корреляция**: сквозной путь объявления — по `event_id` (поле контракта
> listings.new, ТЗ 4.3). В notifier `event_id` в логах — после PR #124;
> в core-api логирование event_id ещё не реализовано (core-часть карточки 02
> — отдельный PR), поэтому grep по event_id в core-логах вернёт пусто —
> в §5 трассировка core-стороны идёт через SQL по outbox.
> HTTP-запросы — по `X-Request-ID` (в логах `request_id`, ответ отдаёт
> заголовок; после PR #124). Метрики — `GET :8080/metrics` (после PR #125).

Общие команды:

```bash
docker compose logs notifier --tail 100        # JSON-логи
docker compose logs core-api --tail 100
docker compose exec postgres psql -U app -d realestate   # SQL-диагностика
docker compose exec redis redis-cli XPENDING listings.new notifier
```

## 1. Pending растёт (notifier не ест стрим)

**Симптом**: `notifier_stream_pending_events` (после #125) растёт или
`XPENDING` в redis-cli не падает; `notifier_event_process_errors_total`
штампует.

**Вероятные причины** (по частоте):
1. Postgres недоступен / таймауты — воркеры не подтверждают (ТЗ 4.3,
   запись остаётся в pending — это корректное поведение при сбое).
2. Consumer упал целиком (container unhealthy/перезапускается).
3. Яд: событие ломает обработку — но тогда оно уйдёт в listings.dead, см. §2.

**Шаги**:
```bash
# 1. Жив ли postgres и что в логах notifier
docker compose ps
docker compose logs notifier --tail 50 | grep -E '"level":"ERROR"'
# 2. Кто давний (idle) держит pending — форма IDLE + диапазон + count
docker compose exec redis redis-cli XPENDING listings.new notifier IDLE 30000 - + 20
# 3. Точные причины по последним event_id
docker compose logs notifier --tail 200 | grep handle
```

**Починилось, если**: `XPENDING` возвращается к 0, ошибки в логе
прекратились, `notifier_deferred_jobs` не растёт.

## 2. Записи улетают в listings.dead

**Симптом**: `notifier_dead_letter_total` > 0 (после #125); в логе
`bad payload moved to dead stream` с полем `reason`.

**Диагноз**: битый payload в стриме (публикация без валидации — из
/debug/event с кривым JSON или баг публикации core-api). Причина
сохранена в записи dead-стрима.

**Шаги**:
```bash
docker compose exec redis redis-cli XLEN listings.dead
docker compose exec redis redis-cli XRANGE listings.dead - +
# в записи: reason (почему ParseEvent отклонил), data (сам payload), src_id
```
По `reason` чинить источник (валидация публикации). Dead-стрим — не мусорка:
это очередь разбора, записи не удалять, пока причина не понята.

## 3. Отложенные задания копятся (deferred растёт)

**Симптом**: `notifier_deferred_jobs` растёт и не дренажируется
(в db-режиме; в memory-режиме гейдж не обновляется — смотри SQL ниже).

**Вероятные причины**:
1. Честный лимит 30/час на активного пользователя — временно, дренаж
   флешером (каждые 30 с). Норма при всплеске публикаций.
2. Карантин: битые строки `status='failed'` заблокировали пару
   (chat_id, listing_id) — флешер их не берёт.

**Шаги**:
```sql
SELECT status, count(*) FROM notifier.notifications_log GROUP BY status;
-- карантинные строки:
SELECT chat_id, listing_id, sent_at FROM notifier.notifications_log
WHERE status = 'failed';
-- очередь отложенных (db-режим): ближайшие available_at
SELECT chat_id, listing_id, available_at FROM notifier.notifications_log
WHERE status = 'deferred' ORDER BY available_at LIMIT 10;
```
Карантинные строки — ручной разбор причины (см. логи флешера), затем
решение по паре (ТЗ 4.3: «ошибки с причиной» — после миграции 0003,
issue #111, причина будет в колонке `reason`).

**Починилось, если**: deferred уходит к 0 за пределами часа; failed-строки
разобраны.

## 4. Core API отдаёт 500 на POST /api/v1/listings

**Шаги**:
```bash
docker compose logs core-api --tail 50 | grep -iE 'error|warning'
# (core-api пока пишет plain-text логи; JSON-формат появится с core-частью
# карточки 02 — до того паттерн "level":"ERROR" вернёт пусто)
# Частая причина на чистом volume: схема core не накатилась.
# Миграции в compose станут автоматическими с issue #115 (one-shot
# сервис db-migrate); до того — вручную:
docker compose exec postgres psql -U app -d realestate -c \
  "SELECT version_num FROM public.alembic_version;"
cd core-api && alembic current && alembic upgrade head
```
Если `relation ... does not exist` — это схема; если 422 — это валидация,
не инцидент (см. `message` в ответе).

## 5. «Объявление не дошло до Telegram»

**Шаги**: трассировка по event_id от публикации до доставки:
```bash
# 1. Событие опубликовано outbox'ом? (core-api event_id в логах не пишет,
#    смотрим напрямую в БД — после мержа #96; до него outbox in-memory
#    и трассировка начинается с notifier-стороны)
docker compose exec postgres psql -U app -d realestate -c \
  "SELECT id, published_at FROM core.outbox_events
   WHERE payload->>'event_id' = '<event_id>';"
# 2. Notifier его взял и обработал? (после PR #124)
docker compose logs notifier | grep <event_id>
# 3. Совпадение по подпискам было? Что решил dispatcher?
# ищем match/deferred/rate limited/unknown по этому event_id
```
Ветвление: пусто в `outbox_events` → событие не дошло до outbox (проблема
POST-пути, см. §4); строка есть, `published_at IS NULL` → outbox не
опубликовал в Redis (backlog считается тем же SQL; метрики outbox —
отдельный PR, core-часть карточки 01); есть в notifier, нет match →
подписки не совпали (проверить `/list` у пользователя); есть match,
статус `unknown` в журнале → ошибка Telegram, авто-повтора нет по
contracts.md §2 (повтор — явной повторной публикацией); статус
`deferred` → лимит 30/час, ждём флешер (§3).

## 6. Notifier unhealthy / /health/ready 503

**Симптом**: docker-compose healthcheck красный.

```bash
curl -s http://127.0.0.1:8080/health/ready | jq
# поля: redis, dicts, db — что деградировало
```
- `redis:false` → Redis down/сеть; notifier продолжит: pending не теряется,
  outbox-задания копируются у core (НФТ-7).
- `dicts:false` → справочники core-api недоступны (ожидаемо до мержа
  справочников, известное расхождение №2; при живом core — проверить его).
- `db:false` → Postgres; при memory-режиме поле не валидит (pool == nil).

## 7. Redis down

**Симптом**: 503 по /health/ready, `xreadgroup`/`xack` WARN'ы в notifier,
outbox-ретраи в core.

Поведение по НФТ-7 (нормальное, не инцидент потери): notifier повторяет
после восстановления, pending сохранён; core пишет outbox в PG и
публикует, когда Redis вернётся; кэш-версия (после #107) защищает от
устаревших списков. Шаги: поднять Redis, убедиться, что pending/deferred
дренажируются, ошибки в логах прекратились.

## 8. Бэкап перестал обновляться (после PR #126)

**Симптом**: mtime `/backups/.last-success` в volume старше 26 часов;
`docker compose logs db-backup` показывает ОШИБКА дампа.

```bash
docker compose exec db-backup ls -l /backups/.last-success
docker compose logs db-backup --tail 30
```
Частые причины: postgres unhealthy (команды — §1: `docker compose ps`,
`docker compose logs postgres`), volume `backups` переполнен (retention
чистит сам — проверить права), пароль `POSTGRES_PASSWORD` не совпал после
ротации (см. docs/db-backup-restore.md).

Автоматического алерта по маркеру пока нет — он появится с карточкой 03;
до того проверять вручную командой выше.

## Что НЕ является инцидентом

- `dicts:false` до мержа справочников (ФТ-2.4, известное расхождение №2).
- 429 от Telegram с Retry-After — лимит Telegram, notifier ждёт и
  повторяет (ТЗ 4.3), счётчик `notifier_telegram_retry_after_total`.
- Одиночный `WARN` self-healing циклов (xreadgroup/xlen/xautoclaim) —
  тревожная серия, а не единичный.

## Обновление документа

После каждой крупной аварии: добавить/уточнить симптом → шаги →
«починилось, если». Runbook без полей «починилось, если» — не runbook.
