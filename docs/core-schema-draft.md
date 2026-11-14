# Проект схемы `core` — черновик для обсуждения

Автор: Илья (схема БД). Статус: **черновик, не согласован**. Реализация — только после
согласования с Тимуром (модели SQLAlchemy, запросы) и Даниилом (outbox-интеграция).
Ссылки на ТЗ v1.1: разделы 3.1–3.3, 3.7, 4.2, 4.3. Схема меняется только миграциями
Alembic (НФТ-6), справочники заполняются миграциями (ТЗ 3.2, 3.7).

## Таблицы

### countries (ТЗ 3.7)

| колонка | тип | ограничения |
|---|---|---|
| code | CHAR(2) | PK, ISO 3166-1 alpha-2 верхним регистром |
| name | TEXT | NOT NULL UNIQUE |

### cities (ТЗ 3.7: уникальность города внутри страны)

| колонка | тип | ограничения |
|---|---|---|
| id | BIGINT IDENTITY | PK |
| country_code | CHAR(2) | NOT NULL FK → countries.code |
| name | TEXT | NOT NULL, UNIQUE (country_code, name) — каноническое латинское |

### city_name_aliases (ТЗ 4.2: «таблица либо поле по решению Ильи» — выбрана таблица)

| колонка | тип | ограничения |
|---|---|---|
| id | BIGINT IDENTITY | PK |
| city_id | BIGINT | NOT NULL FK → cities.id ON DELETE CASCADE |
| alias | TEXT | NOT NULL, UNIQUE (alias) |

Аргументы решения: поиск `WHERE alias = lower(trim(вход))` бьёт по индексу; массив в
поле потребовал бы GIN и не даёт жёсткой гарантии «одна альтернатива ведёт ровно к
одному городу» (ТЗ 3.7 запрещает сопоставление по названию без страны — уникальность
alias это страхует).

### sources

| колонка | тип | ограничения |
|---|---|---|
| id | BIGINT IDENTITY | PK |
| code | TEXT | NOT NULL UNIQUE ('mock', 'avito') |

### currencies (ТЗ 3.2: исходные курсы задаются миграциями)

| колонка | тип | ограничения |
|---|---|---|
| code | CHAR(3) | PK, ISO 4217 |
| rate_to_usd | NUMERIC(18,8) | NOT NULL CHECK > 0; USD = 1 |

### listings (ТЗ 3.1 + 3.3)

| колонка | тип | ограничения |
|---|---|---|
| id | BIGINT IDENTITY | PK |
| source_id | BIGINT | NOT NULL FK → sources.id |
| external_id | TEXT | NOT NULL; UNIQUE (source_id, external_id) — ТЗ 3.3 |
| url_hash | CHAR(64) | NOT NULL UNIQUE — SHA-256 сериализации (source, external_id); имя историческое, ТЗ 3.3 |
| url | TEXT | NOT NULL |
| deal_type | TEXT | NOT NULL CHECK IN ('rent','sale') |
| country_code | CHAR(2) | NOT NULL FK → countries.code |
| city_id | BIGINT | NOT NULL FK → cities.id |
| price | NUMERIC(18,2) | NOT NULL CHECK > 0 |
| currency_code | CHAR(3) | NOT NULL FK → currencies.code |
| price_usd | BIGINT | NOT NULL CHECK > 0 — ROUND_HALF_UP до целого USD (ТЗ 3.2) |
| rooms | INT | NOT NULL CHECK >= 0 — студия = 0, NULL запрещён (ТЗ 3.1) |
| title, address, floor, description | TEXT | NULL |
| area_m2 | NUMERIC(8,2) | CHECK NULL OR > 0 |
| published_at | TIMESTAMPTZ | NULL |
| content_hash | CHAR(64) | NOT NULL — SHA-256(city+country+address+rooms+area+deal_type), считает Core API при вставке (ТЗ 3.3) |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT now() |

### listing_photos (решение: отдельная таблица, не массив/JSONB)

| колонка | тип | ограничения |
|---|---|---|
| id | BIGINT IDENTITY | PK |
| listing_id | BIGINT | NOT NULL FK → listings.id ON DELETE CASCADE |
| url | TEXT | NOT NULL |
| sort | INT | NOT NULL DEFAULT 0 |

### listing_duplicates (ТЗ 3.3: связь «возможный дубль», обе записи сохраняются)

| колонка | тип | ограничения |
|---|---|---|
| listing_a_id | BIGINT | NOT NULL FK → listings.id ON DELETE CASCADE |
| listing_b_id | BIGINT | NOT NULL FK → listings.id ON DELETE CASCADE |
| content_hash | CHAR(64) | NOT NULL |
| — | — | CHECK (listing_a_id < listing_b_id), PK (listing_a_id, listing_b_id) |

Индекс по content_hash — поиск пар по совпадению хэша.

### outbox_events (ТЗ 4.3: transactional outbox, объявление + задание в одной транзакции)

| колонка | тип | ограничения |
|---|---|---|
| id | BIGINT IDENTITY | PK |
| event_id | UUID | NOT NULL UNIQUE — устойчивый UUID задания (ФТ-2.5) |
| kind | TEXT | NOT NULL CHECK IN ('listing_new','cache_invalidate') |
| payload | JSONB | NOT NULL — для listing_new: контрактный JSON события (ТЗ 4.3) |
| listing_id | BIGINT | NULL FK → listings.id — для cache_invalidate может не привязываться |
| published_at | TIMESTAMPTZ | NULL — отметка после XADD; NULL = задание ждёт |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT now() |

Частичный индекс `ON (published_at) WHERE published_at IS NULL` — очередь фоновой
задачи Core API. Дубль POST не создаёт нового outbox-задания (ТЗ 4.3) — гарантия
уровня кода, не БД.

## Индексы (ТЗ 4.2)

- `(deal_type, city_id, price_usd)` — фильтры ФТ-2.2
- `(created_at DESC)` — сортировка по умолчанию date_desc
- UNIQUE из 3.3 выше
- Индекс по `rooms` и по `area_m2` — вопрос: нужны ли сразу или после анализа
  запросов (НФТ-10, 500+ объявлений) — см. вопросы к Тимуру

## Открытые вопросы (нужны ответы до Alembic-миграции)

1. **Тимуру**: `photos` отдельной таблицей — ок? (альтернатива — JSONB в listings)
2. **Тимуру**: фильтр `area_min/area_max` (ФТ-2.2) подтверждён? Тогда индекс на `area_m2`.
3. **Тимуру**: индекс `rooms` — сразу или после анализа запросов?
4. **Тимуру**: лимиты длины текстовых полей — в Pydantic, БД без VARCHAR(n)? Предлагаю да.
5. **Даниилу**: outbox-порядок — фоновая задача Core API читает
   `WHERE published_at IS NULL`, XADD, затем UPDATE published_at (ТЗ 4.3). Формат
   payload = контракт listings.new — контракты не меняются.
6. **Даниилу**: `cache_invalidate` в той же таблице outbox (ТЗ 3.5: «событие
   инвалидации сохраняется вместе с outbox-заданием») — ок? Consumer инвалидации —
   только Core API сам (обновление cache:version).

## План реализации (после согласования)

1. Alembic init в core-api (env.py на asyncpg, alembic.ini; без `create_all` — НФТ-6).
2. Миграция 0001: все таблицы выше + seed справочников (страны, города, алиасы,
   курсы — ТЗ 3.2/3.7 «задаются миграциями»).
3. Ручная вычитка автогенерации (правило AGENTS.md), ветка `feat/db-core-schema`, PR.
4. Следом — CI-джоба `alembic upgrade head` по шаблону из PR #23.
5. Роли/права per-service (6.1) — отдельная миграция после запуска этапа.
