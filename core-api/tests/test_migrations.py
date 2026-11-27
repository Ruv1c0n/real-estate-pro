"""Интеграционный тест миграций Alembic (зона Ильи: миграции — ТЗ 6.1, НФТ-6).

Запускается ТОЛЬКО при заданной переменной окружения MIGRATION_TEST_DSN —
DSN администратора **изолированного** сервера PostgreSQL (к служебной базе
`postgres`), например:

    MIGRATION_TEST_DSN=postgresql://app:app@127.0.0.1:5432/postgres

Требуется именно изолированный сервер (одноразовый `docker run postgres:16`
или services-контейнер CI): роли per-service глобальны для кластера, и на
общем дев-инстансе их DROP на шаге downgrade заблокируют гранты из других
баз. На каждом прогоне тест создаёт одноразовую базу, гоняет миграции
в обе стороны (upgrade head / downgrade base / upgrade head) и проверяет
результат: состав схемы core, версию (сверяется с фактическим head),
роли per-service и изоляцию схем. Без переменной окружения — skip.
"""

import asyncio
from pathlib import Path

import asyncpg

CORE_API_DIR = Path(__file__).resolve().parents[1]

# Тест адаптивен к составу миграций в текущем checkout: на ветке с одной
# 0001 head = "0001" (роли не проверяются), в стеке с 0002 — полная проверка.
HAS_0002 = (
    CORE_API_DIR / "alembic" / "versions" / "0002_roles_per_service.py"
).exists()

HAS_0003 = (
    CORE_API_DIR / "alembic" / "versions" / "0003_fk_indexes_checks.py"
).exists()

HAS_0004 = (
    CORE_API_DIR / "alembic" / "versions" / "0004_roles_notifier_ddl.py"
).exists()

# Стенд-заглушка для 0004: объект в схеме notifier, созданный общей ролью
# до миграций (на реальном стенде его создаёт раннер под app). 0004 обязан
# перенести его владение роли notifier; identity-последовательность проверяет,
# что перенос доходит и до принадлежащих таблице последовательностей.
PROBE_TABLE = "notifier.migration_probe_ownership"


async def _create_probe(conn: asyncpg.Connection) -> None:
    await conn.execute("CREATE SCHEMA IF NOT EXISTS notifier")
    await conn.execute(
        "CREATE TABLE notifier.migration_probe_ownership ("
        " id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY)"
    )


async def _verify_schema(dsn: str, head: str, *, expect_seeded: bool) -> None:
    conn = await asyncpg.connect(dsn)
    try:
        tables = await conn.fetch(
            "SELECT table_name FROM information_schema.tables"
            " WHERE table_schema = 'core'"
        )
        assert len(tables) == 9, f"ожидались 9 таблиц core, получено {len(tables)}"
        version = await conn.fetchval("SELECT version_num FROM public.alembic_version")
        assert version == head, f"версия {version}, ожидался head {head}"
        if HAS_0002:
            roles = await conn.fetch(
                "SELECT rolname FROM pg_roles WHERE rolname IN ('core_api', 'notifier')"
            )
            assert len(roles) == 2, "роли per-service не созданы"
            assert await conn.fetchval(
                "SELECT has_table_privilege('core_api', 'core.listings', 'SELECT')"
            )
            assert await conn.fetchval(
                "SELECT has_schema_privilege('notifier', 'notifier', 'USAGE')"
            )
            assert not await conn.fetchval(
                "SELECT has_schema_privilege('notifier', 'core', 'USAGE')"
            )
            assert not await conn.fetchval(
                "SELECT has_schema_privilege('core_api', 'notifier', 'USAGE')"
            )
        if HAS_0003:
            indexes = await conn.fetch(
                "SELECT indexname FROM pg_indexes WHERE schemaname = 'core'"
                " AND indexname IN ('listing_photos_listing_id_idx',"
                " 'listing_duplicates_listing_b_id_idx',"
                " 'outbox_events_listing_id_idx',"
                " 'city_name_aliases_city_id_idx')"
            )
            assert len(indexes) == 4, "индексы FK-колонок не созданы"
            checks = await conn.fetch(
                "SELECT conname FROM pg_constraint WHERE conname IN"
                " ('outbox_events_kind_listing_chk',"
                " 'city_name_aliases_alias_norm_chk',"
                " 'countries_code_upper_chk', 'currencies_code_upper_chk',"
                " 'listings_url_hash_len_chk', 'listings_content_hash_len_chk',"
                " 'listing_duplicates_content_hash_len_chk')"
            )
            assert len(checks) == 7, "CHECK-доки 0003 не созданы"
        if HAS_0004:
            # 0004: DDL-право роли notifier на свою схему (issue #135)
            assert await conn.fetchval(
                "SELECT has_schema_privilege('notifier', 'notifier', 'CREATE')"
            )
            # CREATE на уровне БД не выдаётся (шире пределов ТЗ 6.5)
            assert not await conn.fetchval(
                "SELECT has_database_privilege('notifier', current_database(), 'CREATE')"
            )
            # владение самой схемой остаётся у bootstrap-роли
            schema_owner = await conn.fetchval(
                "SELECT pg_get_userbyid(n.nspowner) FROM pg_namespace n"
                " WHERE n.nspname = 'notifier'"
            )
            assert schema_owner != "notifier", (
                "владение схемой notifier не должно уходить роли notifier"
            )
            # владение существующими объектами перенесено роли notifier
            probe_owner = await conn.fetchval(
                "SELECT pg_get_userbyid(c.relowner) FROM pg_class c"
                f" WHERE c.oid = '{PROBE_TABLE}'::regclass"
            )
            assert probe_owner == "notifier", (
                f"владение probe-таблицей не перенесено (получено: {probe_owner})"
            )
            assert (
                await conn.fetchval(
                    "SELECT pg_get_userbyid(c.relowner) FROM pg_class c"
                    " WHERE c.oid = 'notifier.migration_probe_ownership_id_seq'::regclass"
                )
                == "notifier"
            ), "владение identity-последовательностью не перенесено"
            # эмуляция раннера notifier под сервисной ролью: создаёт и удаляет
            # таблицу своей схемы (на 0002 без 0004 падало с permission denied)
            probe_conn = await asyncpg.connect(dsn)
            try:
                await probe_conn.execute("SET ROLE notifier")
                await probe_conn.execute(
                    "CREATE TABLE notifier.migration_probe_ddl (id INT)"
                )
                await probe_conn.execute("DROP TABLE notifier.migration_probe_ddl")
            finally:
                await probe_conn.close()
        if expect_seeded:
            assert await conn.fetchval("SELECT count(*) FROM core.countries") == 5
            assert (
                await conn.fetchval("SELECT count(*) FROM core.city_name_aliases") == 14
            )
    finally:
        await conn.close()


def test_migrations_full_cycle(temporary_database, run_alembic) -> None:
    asyncio.run(_lifecycle(temporary_database, run_alembic))


async def _lifecycle(temporary_database, run_alembic) -> None:
    async with temporary_database() as url:
        dsn = url.render_as_string(hide_password=False)
        heads = await run_alembic(url, "heads")
        head = heads.strip().split()[0]

        if HAS_0004:
            # объект общей роли в схеме notifier до миграций — как на стенде,
            # где раннер уже создал таблицы под app (issue #135)
            conn = await asyncpg.connect(dsn)
            try:
                await _create_probe(conn)
            finally:
                await conn.close()

        await run_alembic(url, "upgrade", "head")
        await _verify_schema(dsn, head, expect_seeded=True)

        await run_alembic(url, "downgrade", "base")
        conn = await asyncpg.connect(dsn)
        try:
            leftover = await conn.fetchval(
                "SELECT count(*) FROM information_schema.tables"
                " WHERE table_schema = 'core'"
            )
            assert leftover == 0, "после downgrade base остались таблицы core"
            if HAS_0002:
                roles_left = await conn.fetchval(
                    "SELECT count(*) FROM pg_roles"
                    " WHERE rolname IN ('core_api', 'notifier')"
                )
                assert roles_left == 0, (
                    "роли per-service не удалены после downgrade base"
                )
                if HAS_0004:
                    probe_owner = await conn.fetchval(
                        "SELECT pg_get_userbyid(c.relowner) FROM pg_class c"
                        f" WHERE c.oid = '{PROBE_TABLE}'::regclass"
                    )
                    assert probe_owner != "notifier", (
                        "после downgrade владение probe-таблицей не вернулось "
                        f"от notifier (получено: {probe_owner}) — DROP ROLE "
                        "notifier в downgrade 0002 на таком состоянии упадёт"
                    )
        finally:
            await conn.close()

        await run_alembic(url, "upgrade", "head")
        await _verify_schema(dsn, head, expect_seeded=True)

        # Повторный upgrade на текущей версии должен быть безопасен.
        await run_alembic(url, "upgrade", "head")
        await _verify_schema(dsn, head, expect_seeded=True)
