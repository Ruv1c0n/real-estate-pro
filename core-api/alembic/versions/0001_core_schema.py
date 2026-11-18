"""Начальная схема core: справочники, listings, дубли, outbox + seed.

Схема по docs/core-schema-draft.md (согласован в PR #66) и ТЗ v1.1:
3.1-3.3, 3.5, 3.7, 4.2, 4.3; НФТ-6 (схема меняется только миграциями).

Требования ревью PR #66: индекс на listings.content_hash; sources.name;
floor — INT; составной FK (city_id, country_code) -> cities (id, country_code);
алиасы городов хранятся нормализованными (lower + trim); чтение outbox
планируется с FOR UPDATE SKIP LOCKED; cache_invalidate обрабатывает сам
core-api без stream (INCR cache:version). Индексы rooms и area_m2 — сразу.

Revision ID: 0001
Revises: -
Create Date: 2026-11-17
"""

from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op
from sqlalchemy.dialects import postgresql

# revision identifiers, used by Alembic.
revision: str = "0001"
down_revision: str | None = None
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.execute("CREATE SCHEMA IF NOT EXISTS core")

    # --- Справочники (ТЗ 3.7, заполняются миграциями) ---

    op.create_table(
        "countries",
        sa.Column("code", sa.CHAR(2), primary_key=True),
        sa.Column("name", sa.Text(), nullable=False, unique=True),
        schema="core",
        comment="Страны: ISO 3166-1 alpha-2, ТЗ 3.7",
    )

    op.create_table(
        "cities",
        sa.Column("id", sa.BigInteger(), sa.Identity(), primary_key=True),
        sa.Column(
            "country_code",
            sa.CHAR(2),
            sa.ForeignKey("core.countries.code", name="cities_country_fk"),
            nullable=False,
        ),
        sa.Column("name", sa.Text(), nullable=False),
        sa.UniqueConstraint("country_code", "name", name="cities_country_name_uq"),
        # Цель составного FK из listings: город строго внутри своей страны
        sa.UniqueConstraint("id", "country_code", name="cities_id_country_uq"),
        schema="core",
        comment="Города: каноническое латинское название внутри страны, ТЗ 3.7",
    )

    op.create_table(
        "city_name_aliases",
        sa.Column("id", sa.BigInteger(), sa.Identity(), primary_key=True),
        sa.Column(
            "city_id",
            sa.BigInteger(),
            sa.ForeignKey(
                "core.cities.id",
                name="city_name_aliases_city_fk",
                ondelete="CASCADE",
            ),
            nullable=False,
        ),
        # В колонке лежат уже нормализованные значения (lower + trim при
        # вставке) — поиск WHERE alias = lower(trim(вход)) бьёт по индексу
        sa.Column("alias", sa.Text(), nullable=False, unique=True),
        schema="core",
        comment="Альтернативные написания городов, ТЗ 3.7 / 4.2 (решение Ильи)",
    )

    op.create_table(
        "sources",
        sa.Column("id", sa.BigInteger(), sa.Identity(), primary_key=True),
        sa.Column("code", sa.Text(), nullable=False, unique=True),
        sa.Column("name", sa.Text(), nullable=False),
        schema="core",
        comment="Источники объявлений (mock, avito)",
    )

    op.create_table(
        "currencies",
        sa.Column("code", sa.CHAR(3), primary_key=True),
        sa.Column(
            "rate_to_usd",
            sa.Numeric(18, 8),
            sa.CheckConstraint("rate_to_usd > 0", name="currencies_rate_chk"),
            nullable=False,
        ),
        schema="core",
        # Стоимость единицы валюты в USD, USD = 1; исходные значения задаются
        # миграциями и в демо не меняются (ТЗ 3.2)
        comment="Курсы к USD, ТЗ 3.2",
    )

    # --- Объявления (ТЗ 3.1 + 3.3) ---

    op.create_table(
        "listings",
        sa.Column("id", sa.BigInteger(), sa.Identity(), primary_key=True),
        sa.Column(
            "source_id",
            sa.BigInteger(),
            sa.ForeignKey("core.sources.id", name="listings_source_fk"),
            nullable=False,
        ),
        sa.Column("external_id", sa.Text(), nullable=False),
        sa.Column("url_hash", sa.CHAR(64), nullable=False),
        sa.Column("url", sa.Text(), nullable=False),
        sa.Column(
            "deal_type",
            sa.Text(),
            sa.CheckConstraint(
                "deal_type IN ('rent', 'sale')", name="listings_deal_type_chk"
            ),
            nullable=False,
        ),
        sa.Column(
            "country_code",
            sa.CHAR(2),
            sa.ForeignKey("core.countries.code", name="listings_country_fk"),
            nullable=False,
        ),
        sa.Column("city_id", sa.BigInteger(), nullable=False),
        sa.Column(
            "price",
            sa.Numeric(18, 2),
            sa.CheckConstraint("price > 0", name="listings_price_chk"),
            nullable=False,
        ),
        sa.Column(
            "currency_code",
            sa.CHAR(3),
            sa.ForeignKey("core.currencies.code", name="listings_currency_fk"),
            nullable=False,
        ),
        sa.Column(
            "price_usd",
            sa.BigInteger(),
            sa.CheckConstraint("price_usd > 0", name="listings_price_usd_chk"),
            nullable=False,
        ),
        sa.Column(
            "rooms",
            sa.Integer(),
            # 0 = студия, NULL запрещён (ТЗ 3.1)
            sa.CheckConstraint("rooms >= 0", name="listings_rooms_chk"),
            nullable=False,
        ),
        sa.Column("floor", sa.Integer(), nullable=True),
        sa.Column("title", sa.Text(), nullable=True),
        sa.Column("address", sa.Text(), nullable=True),
        sa.Column("description", sa.Text(), nullable=True),
        sa.Column(
            "area_m2",
            sa.Numeric(8, 2),
            sa.CheckConstraint(
                "area_m2 IS NULL OR area_m2 > 0", name="listings_area_m2_chk"
            ),
            nullable=True,
        ),
        sa.Column("published_at", sa.DateTime(timezone=True), nullable=True),
        # SHA-256(city+country+address+rooms+area+deal_type), считает Core API
        # при вставке; по нему ищутся «возможные дубли» (ТЗ 3.3)
        sa.Column("content_hash", sa.CHAR(64), nullable=False),
        sa.Column(
            "created_at",
            sa.DateTime(timezone=True),
            nullable=False,
            server_default=sa.text("now()"),
        ),
        sa.UniqueConstraint(
            "source_id", "external_id", name="listings_source_external_uq"
        ),
        # url_hash = SHA-256 от сериализации пары (source, external_id) —
        # имя историческое, ТЗ 3.3
        sa.UniqueConstraint("url_hash", name="listings_url_hash_uq"),
        # Город принадлежит стране — гарантирует БД, не только код
        sa.ForeignKeyConstraint(
            ["city_id", "country_code"],
            ["core.cities.id", "core.cities.country_code"],
            name="listings_city_country_fk",
        ),
        schema="core",
    )

    op.create_table(
        "listing_photos",
        sa.Column("id", sa.BigInteger(), sa.Identity(), primary_key=True),
        sa.Column(
            "listing_id",
            sa.BigInteger(),
            sa.ForeignKey(
                "core.listings.id",
                name="listing_photos_listing_fk",
                ondelete="CASCADE",
            ),
            nullable=False,
        ),
        sa.Column("url", sa.Text(), nullable=False),
        sa.Column("sort", sa.Integer(), nullable=False, server_default=sa.text("0")),
        schema="core",
        comment="Фотографии объявления, отдельной таблицей (решение Тимура)",
    )

    op.create_table(
        "listing_duplicates",
        sa.Column(
            "listing_a_id",
            sa.BigInteger(),
            sa.ForeignKey(
                "core.listings.id",
                name="listing_duplicates_a_fk",
                ondelete="CASCADE",
            ),
            primary_key=True,
        ),
        sa.Column(
            "listing_b_id",
            sa.BigInteger(),
            sa.ForeignKey(
                "core.listings.id",
                name="listing_duplicates_b_fk",
                ondelete="CASCADE",
            ),
            primary_key=True,
        ),
        sa.Column("content_hash", sa.CHAR(64), nullable=False),
        sa.CheckConstraint(
            "listing_a_id < listing_b_id", name="listing_duplicates_pair_chk"
        ),
        schema="core",
        # Связь «возможный дубль»: обе записи сохраняются (ТЗ 3.3)
        comment="Связи «возможный дубль», ТЗ 3.3",
    )

    # --- Transactional outbox (ТЗ 4.3) ---

    op.create_table(
        "outbox_events",
        sa.Column("id", sa.BigInteger(), sa.Identity(), primary_key=True),
        # Устойчивый UUID задания, не меняется при повторной публикации (ФТ-2.5)
        sa.Column(
            "event_id", postgresql.UUID(as_uuid=True), nullable=False, unique=True
        ),
        sa.Column(
            "kind",
            sa.Text(),
            sa.CheckConstraint(
                "kind IN ('listing_new', 'cache_invalidate')",
                name="outbox_events_kind_chk",
            ),
            nullable=False,
        ),
        # Для listing_new — контрактный JSON события (ТЗ 4.3)
        sa.Column("payload", postgresql.JSONB(), nullable=False),
        sa.Column(
            "listing_id",
            sa.BigInteger(),
            sa.ForeignKey("core.listings.id", name="outbox_events_listing_fk"),
            nullable=True,
        ),
        sa.Column("published_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column(
            "created_at",
            sa.DateTime(timezone=True),
            nullable=False,
            server_default=sa.text("now()"),
        ),
        schema="core",
        # Объявление и задание публикации — одна транзакция (ТЗ 4.3);
        # cache_invalidate обрабатывает сам core-api без stream
        comment="Transactional outbox: публикация listings.new и инвалидация кэша",
    )

    # --- Индексы (ТЗ 4.2 + ревью PR #66) ---

    # Фильтры ФТ-2.2: тип сделки, город, цена в USD
    op.create_index(
        "listings_deal_city_price_idx",
        "listings",
        ["deal_type", "city_id", "price_usd"],
        schema="core",
    )
    # Сортировка по умолчанию date_desc
    op.create_index(
        "listings_created_at_idx",
        "listings",
        [sa.text("created_at DESC")],
        schema="core",
    )
    # Поиск пар-дублей не должен быть full scan (ревью Даниила)
    op.create_index(
        "listings_content_hash_idx", "listings", ["content_hash"], schema="core"
    )
    op.create_index("listings_rooms_idx", "listings", ["rooms"], schema="core")
    op.create_index("listings_area_m2_idx", "listings", ["area_m2"], schema="core")
    op.create_index(
        "listing_duplicates_content_hash_idx",
        "listing_duplicates",
        ["content_hash"],
        schema="core",
    )
    # Очередь фоновой задачи Core API: чтение с FOR UPDATE SKIP LOCKED
    op.create_index(
        "outbox_events_pending_idx",
        "outbox_events",
        ["published_at"],
        schema="core",
        postgresql_where=sa.text("published_at IS NULL"),
    )

    # --- Seed справочников (ТЗ 3.2, 3.7: заполняются миграциями) ---

    # Страны — минимальный набор под синтетический дамп (ТЗ 3.4: >= 5 стран);
    # расширяется миграциями, автоматическое создание из входящих
    # объявлений не выполняется (ТЗ 3.7)
    op.execute(
        """
        INSERT INTO core.countries (code, name) VALUES
            ('BY', 'Belarus'),
            ('RU', 'Russia'),
            ('PL', 'Poland'),
            ('KZ', 'Kazakhstan'),
            ('GE', 'Georgia')
        """
    )

    # Города: канонические латинские названия, уникальны внутри страны.
    # >= 10 городов под ТЗ 3.4; алиасы расширят справочник Димой вместе
    # с mock-дампом
    op.execute(
        """
        INSERT INTO core.cities (country_code, name) VALUES
            ('BY', 'Minsk'),
            ('BY', 'Brest'),
            ('RU', 'Moscow'),
            ('RU', 'Saint Petersburg'),
            ('PL', 'Warsaw'),
            ('PL', 'Krakow'),
            ('KZ', 'Astana'),
            ('KZ', 'Almaty'),
            ('GE', 'Tbilisi'),
            ('GE', 'Batumi')
        """
    )

    # Алиасы — уже нормализованные (lower + trim); примеры из ТЗ 3.7: «Минск», «Мінск»
    op.execute(
        """
        INSERT INTO core.city_name_aliases (city_id, alias)
        SELECT id, v.alias
        FROM core.cities c
        JOIN (VALUES
            ('BY', 'Minsk', 'минск'),
            ('BY', 'Minsk', 'мінск'),
            ('BY', 'Brest', 'брест'),
            ('BY', 'Brest', 'брэст'),
            ('RU', 'Moscow', 'москва'),
            ('RU', 'Saint Petersburg', 'санкт-петербург'),
            ('RU', 'Saint Petersburg', 'спб'),
            ('PL', 'Warsaw', 'варшава'),
            ('PL', 'Krakow', 'краков'),
            ('KZ', 'Astana', 'астана'),
            ('KZ', 'Astana', 'нур-султан'),
            ('KZ', 'Almaty', 'алматы'),
            ('GE', 'Tbilisi', 'тбилиси'),
            ('GE', 'Batumi', 'батуми')
        ) AS v (country_code, name, alias)
            ON v.country_code = c.country_code AND v.name = c.name
        """
    )

    op.execute(
        """
        INSERT INTO core.sources (code, name) VALUES
            ('mock', 'Mock-данные'),
            ('avito', 'Avito')
        """
    )

    # Демонстрационные курсы (ТЗ 3.2: задаются миграциями, в демо не меняются;
    # не реальные биржевые). rate_to_usd — стоимость единицы валюты в USD
    op.execute(
        """
        INSERT INTO core.currencies (code, rate_to_usd) VALUES
            ('USD', 1),
            ('BYN', 0.31),
            ('RUB', 0.011),
            ('PLN', 0.25),
            ('EUR', 1.10),
            ('KZT', 0.0021),
            ('GEL', 0.37)
        """
    )


def downgrade() -> None:
    # Обратный порядок зависимостей; seed и таблицы удаляются вместе со схемой
    op.drop_table("outbox_events", schema="core")
    op.drop_table("listing_duplicates", schema="core")
    op.drop_table("listing_photos", schema="core")
    op.drop_table("listings", schema="core")
    op.drop_table("currencies", schema="core")
    op.drop_table("sources", schema="core")
    op.drop_table("city_name_aliases", schema="core")
    op.drop_table("cities", schema="core")
    op.drop_table("countries", schema="core")
    op.execute("DROP SCHEMA IF EXISTS core CASCADE")
