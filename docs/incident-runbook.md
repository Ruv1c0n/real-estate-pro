# Runbook инцидентов — симптомы → диагноз → действия

> Операционный документ (карточка 09 плана prod-10of10). Цель: по симптому
> в логах/метриках добраться до причины и понять, «починилось ли» — без
> чтения исходников на 3 часа ночи.
>
> **Корреляция**: сквозной путь объявления — по `event_id` (поле контракта
> listings.new, ТЗ 4.3; логируется во всех сервисах после PR #124).
> HTTP-запросы — по `X-Request-ID` (в логах `request_id`, ответ отдаёт
> заголовок). Метрики — `GET :8080/metrics` (после PR #125).

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
# 2. Кто держит pending
docker compose exec redis redis-cli XPENDING listings.new notifier - IDLE 30000
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

**Симптом**: `notifier_deferred_jobs` растёт и не дренажируется.

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
docker compose logs core-api --tail 50 | grep -E '"level":"(ERROR|WARNING)"'
# Частая причина на чистом volume: схема core не накатилась.
# (после мержа PR #115 миграции в compose автоматические; до того — вручную)
docker compose exec postgres psql -U app -d realestate -c \
  "SELECT version_num FROM core.alembic_version;"
cd core-api && alembic current && alembic upgrade head
```
Если `relation ... does not exist` — это схема; если 422 — это валидация,
не инцидент (см. `message` в ответе).

## 5. «Объявление не дошло до Telegram»

**Шаги**: трассировка по event_id от публикации до доставки:
```bash
# 1. Событие вообще опубликовано?
docker compose logs core-api | grep <event_id>
# 2.Notifier его взял и обработал?
docker compose logs notifier | grep <event_id>
# 3. Совпадение по подпискам было? Что решил dispatcher?
# ищем match/deferred/rate limited/unknown по этому event_id
```
Ветвление: нет в core-логах → outbox не опубликовал (см. backlog-метрику,
после мержа #96); есть в notifier, нет match → подписки не совпали (проверить
`/list` у пользователя); есть match, статус `unknown` в журнале → ошибка
Telegram, авто-повтора нет по contracts.md §2 (повтор — явной повторной
публикацией); статус `deferred` → лимит 30/час, ждём флешер (§3).

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
Частые причины: postgres unhealthy (см. §6-команды для него), volume
`backups` переполнен (retention чистит сам — проверить права), пароль
PO не совпал после ротации (см. docs/db-backup-restore.md).

## Что НЕ является инцидентом

- `dicts:false` до мержа справочников (ФТ-2.4, известное расхождение №2).
- 429 от Telegram с Retry-After — лимит Telegram, notifier ждёт и
  повторяет (ТЗ 4.3), счётчик `notifier_telegram_retry_after_total`.
- Одиночный `WARN` self-healing циклов (xreadgroup/xlen/xautoclaim) —
  тревожная серия, а не единичный.

## Обновление документа

После каждой крупной аварии: добавить/уточнить симптом → шаги →
«починилось, если». Runbook без полей «починилось, если» — не runbook.
