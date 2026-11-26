"""FK-индексы и CHECK-доки схемы core (аудит 20.11: P2-23, P2-4, P3-2/3/4).

Issue #120. PostgreSQL не строит индексы на FK-колонки автоматически:
CASCADE-чистка и RI-проверки RI-триггеров сканируют дочерние таблицы
целиком (план чистки/удаления упирается в Seq Scan).

FK-индексы:
- listing_photos.listing_id — CASCADE-чистка фото при удалении объявления;
- listing_duplicates.listing_b_id — PK (listing_a_id, listing_b_id) покрывает
  только ведущую колонку, RI-проверка/CASCADE по listing_b_id шла по скану;
- outbox_events.listing_id — RI-проверка при удалении объявления (FK без
  ondelete — NO ACTION), плюс join'ы outbox на объявления;
- city_name_aliases.city_id — CASCADE-чистка алиасов при удалении города:
  тот же класс, что у listing_photos; в список аудита не попала, добавлена
  по той же причине.

CHECK-доки — фиксируем в БД инварианты, которые раньше держал только код:

- outbox_events: событие listing_new обязано ссылаться на объявление
  (у cache_invalidate listing_id остаётся NULL);
- city_name_aliases: alias нормализован (lower + btrim) — значения мимо
  нормализации не уйдут мимо индекса unique(alias) при поиске;
- countries.code / currencies.code — ISO-коды в верхнем регистре. sources
  сознательно не трогается: коды mock/avito строчные по seed 0001 и
  используются кодом сервисов;
- listings.url_hash / listings.content_hash / listing_duplicates.content_hash:
  CHAR(64) добивает короткие значения пробелами (bpchar-паддинг), поэтому
  длину фиксирует char_length = 64 — недобитые значения отсекаются.

Revision ID: 0003
Revises: 0002
Create Date: 2026-11-23
"""

from collections.abc import Sequence

from alembic import op

# revision identifiers, used by Alembic.
revision: str = "0003"
down_revision: str | None = "0002"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    # --- FK-индексы (P2-23) ---
    op.create_index(
        "listing_photos_listing_id_idx",
        "listing_photos",
        ["listing_id"],
        schema="core",
    )
    op.create_index(
        "listing_duplicates_listing_b_id_idx",
        "listing_duplicates",
        ["listing_b_id"],
        schema="core",
    )
    op.create_index(
        "outbox_events_listing_id_idx",
        "outbox_events",
        ["listing_id"],
        schema="core",
    )
    op.create_index(
        "city_name_aliases_city_id_idx",
        "city_name_aliases",
        ["city_id"],
        schema="core",
    )

    # --- CHECK-доки (P2-4, P3-2/3/4) ---
    op.create_check_constraint(
        "outbox_events_kind_listing_chk",
        "outbox_events",
        "kind <> 'listing_new' OR listing_id IS NOT NULL",
        schema="core",
    )
    op.create_check_constraint(
        "city_name_aliases_alias_norm_chk",
        "city_name_aliases",
        "alias = lower(btrim(alias))",
        schema="core",
    )
    op.create_check_constraint(
        "countries_code_upper_chk",
        "countries",
        "code = upper(code)",
        schema="core",
    )
    op.create_check_constraint(
        "currencies_code_upper_chk",
        "currencies",
        "code = upper(code)",
        schema="core",
    )
    op.create_check_constraint(
        "listings_url_hash_len_chk",
        "listings",
        "char_length(url_hash) = 64",
        schema="core",
    )
    op.create_check_constraint(
        "listings_content_hash_len_chk",
        "listings",
        "char_length(content_hash) = 64",
        schema="core",
    )
    op.create_check_constraint(
        "listing_duplicates_content_hash_len_chk",
        "listing_duplicates",
        "char_length(content_hash) = 64",
        schema="core",
    )


def downgrade() -> None:
    # Обратный порядок upgrade; таблицы не затрагиваются
    op.drop_constraint(
        "listing_duplicates_content_hash_len_chk",
        "listing_duplicates",
        schema="core",
        type_="check",
    )
    op.drop_constraint(
        "listings_content_hash_len_chk", "listings", schema="core", type_="check"
    )
    op.drop_constraint(
        "listings_url_hash_len_chk", "listings", schema="core", type_="check"
    )
    op.drop_constraint(
        "currencies_code_upper_chk", "currencies", schema="core", type_="check"
    )
    op.drop_constraint(
        "countries_code_upper_chk", "countries", schema="core", type_="check"
    )
    op.drop_constraint(
        "city_name_aliases_alias_norm_chk",
        "city_name_aliases",
        schema="core",
        type_="check",
    )
    op.drop_constraint(
        "outbox_events_kind_listing_chk",
        "outbox_events",
        schema="core",
        type_="check",
    )
    op.drop_index(
        "city_name_aliases_city_id_idx",
        table_name="city_name_aliases",
        schema="core",
    )
    op.drop_index(
        "outbox_events_listing_id_idx", table_name="outbox_events", schema="core"
    )
    op.drop_index(
        "listing_duplicates_listing_b_id_idx",
        table_name="listing_duplicates",
        schema="core",
    )
    op.drop_index(
        "listing_photos_listing_id_idx",
        table_name="listing_photos",
        schema="core",
    )
