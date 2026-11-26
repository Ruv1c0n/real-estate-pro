-- 0003_reason.sql — причина ошибки доставки в журнале уведомлений
-- (ТЗ 4.3 «постоянные ошибки сохраняются с причиной»; аудит 20.11 P1-6, issue #111).
--
-- reason — текст причины для финальных неуспешных статусов:
--   unknown — ошибка Telegram при отправке (после потолка Retry-After);
--   failed  — карантин PopBatch (битый payload в отложенном задании).
-- Для sent и deferred reason остаётся NULL: доставка успешна, а отложенное
-- задание ещё не финализировано (промежуточные retry причину не пишут —
-- только терминальный исход). CHECK не даёт reason «висеть» на статусах,
-- где он не имеет смысла.
--
-- Использование в коде notifier (зона Даниила, этот файл ничего не ломает,
-- пока код не пишет reason): Record при unknown и UPDATE карантина в
-- PopBatch передают текст ошибки. До этого все строки имеют reason NULL,
-- поэтому на существующем стенде миграция безопасна: колонка добавляется
-- с NULL во всех строках, CHECK выполняется без наполнения.
--
-- Идемпотентность: ADD COLUMN IF NOT EXISTS; для CHECK (синтаксиса
-- ADD CONSTRAINT IF NOT EXISTS в PostgreSQL нет) — DO-блок с проверкой
-- pg_constraint: повторный прогон после сбоя между COMMIT файла и записью
-- версии раннер остаётся no-op вместо «constraint already exists», как
-- в 0001_init.sql.

BEGIN;

ALTER TABLE notifier.notifications_log
    ADD COLUMN IF NOT EXISTS reason TEXT;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'notifications_log_reason_chk'
          AND conrelid = 'notifier.notifications_log'::regclass
    ) THEN
        ALTER TABLE notifier.notifications_log
            ADD CONSTRAINT notifications_log_reason_chk
            CHECK (reason IS NULL OR status IN ('unknown', 'failed'));
    END IF;
END
$$;

COMMIT;
