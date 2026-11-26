# ER-диаграмма схемы core

ТЗ v1.1, раздел 6.1 (ER-диаграмма — зона Ильи). Соответствует миграции
0001 (PR #81). Структура меняется только миграциями — диаграмма
обновляется вместе с ними.

```mermaid
erDiagram
    countries {
        CHAR code PK
        TEXT name UK
    }
    cities {
        BIGINT id PK UK
        CHAR country_code FK
        TEXT name UK
    }
    city_name_aliases {
        BIGINT id PK
        BIGINT city_id FK
        TEXT alias UK
    }
    sources {
        BIGINT id PK
        TEXT code UK
        TEXT name
    }
    currencies {
        CHAR code PK
        NUMERIC rate_to_usd
    }
    listings {
        BIGINT id PK
        BIGINT source_id FK
        TEXT external_id UK
        CHAR url_hash UK
        TEXT url
        TEXT deal_type
        CHAR country_code FK
        BIGINT city_id
        NUMERIC price
        CHAR currency_code FK
        BIGINT price_usd
        INTEGER rooms
        INTEGER floor
        TEXT title
        TEXT address
        TEXT description
        NUMERIC area_m2
        TIMESTAMP_WITH_TIME_ZONE published_at
        CHAR content_hash
        TIMESTAMP_WITH_TIME_ZONE created_at
    }
    listing_photos {
        BIGINT id PK
        BIGINT listing_id FK
        TEXT url
        INTEGER sort
    }
    listing_duplicates {
        BIGINT listing_a_id PK FK
        BIGINT listing_b_id PK FK
        CHAR content_hash
    }
    outbox_events {
        BIGINT id PK
        UUID event_id UK
        TEXT kind
        JSONB payload
        BIGINT listing_id FK
        TIMESTAMP_WITH_TIME_ZONE published_at
        TIMESTAMP_WITH_TIME_ZONE created_at
    }

    countries ||--o{ cities : country_code
    cities ||--o{ city_name_aliases : city_id
    sources ||--o{ listings : source_id
    countries ||--o{ listings : country_code
    currencies ||--o{ listings : currency_code
    cities ||--o{ listings : "city_id + country_code (составной FK)"
    listings ||--o{ listing_photos : listing_id
    listings ||--o{ listing_duplicates : listing_a_id
    listings ||--o{ listing_duplicates : listing_b_id
    listings ||--o{ outbox_events : listing_id
```

Примечания:

- **Составные уникальности**: Mermaid не умеет составные UK, поэтому пометки
  `UK` на колонках читаются с оговоркой (ревью PR #86):
  - `listings.external_id` уникален только **внутри источника** — составной
    `UNIQUE (source_id, external_id)`, не глобально;
  - `cities.name` уникален только **внутри страны** — составной
    `UNIQUE (country_code, name)`, не глобально;
  - `cities.id` в паре с `country_code` (`UNIQUE (id, country_code)`) —
    цель составного FK, не самостоятельное ограничение.
- **Составной FK** `listings (city_id, country_code) → cities (id, country_code)`
  гарантирует на уровне БД, что выбранный город принадлежит указанной стране
  (ревью PR #66); у `cities` для этого есть `UNIQUE (id, country_code)`.
- `alembic_version` (таблица версий Alembic в `public`) в диаграмму не входит —
  это служебные метаданные инструмента, не сущность бизнес-схемы.
- Связи дублей: `listing_duplicates` хранит пары «возможный дубль» (ТЗ 3.3),
  обе записи объявлений сохраняются.

## Как перегенерировать

```bash
# 1. DDL без подключения к БД (из каталога core-api)
cd core-api && alembic upgrade head --sql > core-schema.sql
# 2. Генератор Mermaid (scripts/tools/sql-to-er.py, PR #76)
python -I scripts/tools/sql-to-er.py core-schema.sql
```

Из вывода генератора: удалить блок `alembic_version` (служебная таблица) и
добавить строку связи составного FK `cities ||--o{ listings` — парсер
генератора пока не разбирает составные FK (уточнение в PR #76); после
доработки парсера шаг с ручной строкой уйдёт.

## Схема notifier

Диаграмма notifier вынесена в отдельный файл —
[er-diagram-notifier.md](er-diagram-notifier.md): миграции notifier
меняются независимо от core, отдельный файл обновляется вместе с ними,
не трогая этот.

