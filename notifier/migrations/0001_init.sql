-- 0001_init.sql — начальная схема notifier.
-- Заменяет хранение в памяти процесса (store.Memory, notify.Journal, очередь отложенных).
--
-- IF NOT EXISTS у таблиц и индексов: применение учитывает версии раннер
-- (notifier.schema_migrations), но если применение файла прошло, а запись
-- версии не успела (сбой между COMMIT файла и INSERT версии), повторный
-- запуск остаётся безопасным no-op вместо «already exists» и crash-loop.
-- Результирующая схема от этого не меняется.

BEGIN;

CREATE SCHEMA IF NOT EXISTS notifier;

-- Пользователи Telegram-бота. /start создаёт запись, /stop удаляет её
-- вместе с подписками и журналом (ON DELETE CASCADE), ФТ-3.1.
CREATE TABLE IF NOT EXISTS notifier.users (
    chat_id    BIGINT      PRIMARY KEY,           -- Telegram chat_id
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Подписки: сохранённые поиски. NULL = «любое значение».
CREATE TABLE IF NOT EXISTS notifier.subscriptions (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    chat_id       BIGINT      NOT NULL
                  REFERENCES notifier.users (chat_id) ON DELETE CASCADE,
    deal_type     TEXT,                            -- rent / sale
    country       TEXT,                            -- ISO alpha-2, например BY
    city          TEXT,                            -- каноническое название, например Minsk
    rooms         INTEGER,                         -- 0 = студия
    min_price_usd BIGINT,
    max_price_usd BIGINT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT subscriptions_deal_type_chk
        CHECK (deal_type IS NULL OR deal_type IN ('rent', 'sale')),
    CONSTRAINT subscriptions_country_chk
        CHECK (country IS NULL OR country ~ '^[A-Z]{2}$'),
    CONSTRAINT subscriptions_rooms_chk
        CHECK (rooms IS NULL OR rooms >= 0),
    CONSTRAINT subscriptions_min_price_chk
        CHECK (min_price_usd IS NULL OR min_price_usd > 0),
    CONSTRAINT subscriptions_max_price_chk
        CHECK (max_price_usd IS NULL OR max_price_usd > 0),
    CONSTRAINT subscriptions_price_range_chk
        CHECK (min_price_usd IS NULL OR max_price_usd IS NULL
               OR min_price_usd <= max_price_usd),
    -- как в боте: нужен хотя бы один параметр
    CONSTRAINT subscriptions_not_empty_chk
        CHECK (deal_type IS NOT NULL OR country IS NOT NULL OR city IS NOT NULL
               OR rooms IS NOT NULL OR min_price_usd IS NOT NULL
               OR max_price_usd IS NOT NULL)
);

-- /list, /unsubscribe, лимит 10 подписок на пользователя
CREATE INDEX IF NOT EXISTS subscriptions_chat_id_idx ON notifier.subscriptions (chat_id);

-- Журнал уведомлений и отложенные задания (ФТ-3.5).
-- Одна запись на пару (пользователь, объявление) — защита от дублей.
CREATE TABLE IF NOT EXISTS notifier.notifications_log (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    chat_id    BIGINT      NOT NULL
               REFERENCES notifier.users (chat_id) ON DELETE CASCADE,
    listing_id BIGINT      NOT NULL,               -- id объявления в Core API
    event_id   UUID        NOT NULL,               -- event_id из listings.new
    status     TEXT        NOT NULL,
    mode       TEXT        NOT NULL,               -- telegram / log (учитываются отдельно)
    payload    JSONB       NOT NULL,               -- снимок события, нужен для отложенной отправки
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at    TIMESTAMPTZ,

    CONSTRAINT notifications_log_user_listing_uq UNIQUE (chat_id, listing_id),
    CONSTRAINT notifications_log_status_chk
        CHECK (status IN ('deferred', 'sent', 'failed', 'unknown')),
    CONSTRAINT notifications_log_mode_chk
        CHECK (mode IN ('telegram', 'log')),
    CONSTRAINT notifications_log_sent_at_chk
        CHECK (status NOT IN ('sent', 'unknown') OR sent_at IS NOT NULL)
);

-- Лимит «30 уведомлений в скользящий час»: считаем отправленные за последний час
CREATE INDEX IF NOT EXISTS notifications_log_rate_idx
    ON notifier.notifications_log (chat_id, mode, sent_at)
    WHERE sent_at IS NOT NULL;

-- Флашер отложенных заданий забирает самые старые первыми
CREATE INDEX IF NOT EXISTS notifications_log_deferred_idx
    ON notifier.notifications_log (created_at)
    WHERE status = 'deferred';

COMMIT;
