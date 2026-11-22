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
import os
import secrets
import subprocess
import sys
from pathlib import Path
from urllib.parse import urlsplit

import asyncpg
import pytest

CORE_API_DIR = Path(__file__).resolve().parents[1]
ADMIN_DSN = os.environ.get("MIGRATION_TEST_DSN", "")

# Тест адаптивен к составу миграций в текущем checkout: на ветке с одной
# 0001 head = "0001" (роли не проверяются), в стеке с 0002 — полная проверка.
HAS_0002 = (
    CORE_API_DIR / "alembic" / "versions" / "0002_roles_per_service.py"
).exists()

pytestmark = pytest.mark.skipif(
    not ADMIN_DSN,
    reason="MIGRATION_TEST_DSN не задан — тест миграций пропущен",
)


def _components() -> tuple[str, int, str, str]:
    u = urlsplit(ADMIN_DSN)
    return (
        u.hostname or "127.0.0.1",
        u.port or 5432,
        u.username or "app",
        u.password or "",
    )


def _dsn(db: str, driver: str = "postgresql") -> str:
    host, port, user, password = _components()
    auth = f"{user}:{password}@" if password else f"{user}@"
    return f"{driver}://{auth}{host}:{port}/{db}"


def _app_dsn(db: str) -> str:
    # app.config.Settings принимает только postgresql+asyncpg
    return _dsn(db, driver="postgresql+asyncpg")


def _alembic(db: str, *args: str) -> None:
    env = os.environ.copy()
    env["DATABASE_URL"] = _app_dsn(db)
    result = subprocess.run(
        [sys.executable, "-m", "alembic", "-c", "alembic.ini", *args],
        cwd=CORE_API_DIR,
        env=env,
        capture_output=True,
        text=True,
        timeout=180,
        check=False,
    )
    assert result.returncode == 0, (
        f"alembic {' '.join(args)} упал:\n{result.stdout}\n{result.stderr}"
    )


def _head_revision() -> str:
    result = subprocess.run(
        [sys.executable, "-m", "alembic", "-c", "alembic.ini", "heads"],
        cwd=CORE_API_DIR,
        capture_output=True,
        text=True,
        timeout=60,
        check=True,
    )
    # строка вида "0001 (head)"
    return result.stdout.strip().split()[0]


async def _verify_schema(db: str, *, expect_seeded: bool) -> None:
    conn = await asyncpg.connect(dsn=_dsn(db))
    try:
        tables = await conn.fetch(
            "SELECT table_name FROM information_schema.tables"
            " WHERE table_schema = 'core'"
        )
        assert len(tables) == 9, f"ожидались 9 таблиц core, получено {len(tables)}"
        version = await conn.fetchval("SELECT version_num FROM public.alembic_version")
        head = _head_revision()
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
        if expect_seeded:
            assert await conn.fetchval("SELECT count(*) FROM core.countries") == 5
            assert (
                await conn.fetchval("SELECT count(*) FROM core.city_name_aliases") == 14
            )
    finally:
        await conn.close()


async def _lifecycle() -> None:
    db = f"core_mig_{secrets.token_hex(4)}"
    admin = await asyncpg.connect(dsn=_dsn("postgres"))
    try:
        await admin.execute(f'CREATE DATABASE "{db}"')
    finally:
        await admin.close()

    try:
        _alembic(db, "upgrade", "head")
        await _verify_schema(db, expect_seeded=True)

        # полный цикл вниз/вверх: схема и роли должны воспроизводиться
        _alembic(db, "downgrade", "base")
        conn = await asyncpg.connect(dsn=_dsn(db))
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
        finally:
            await conn.close()

        _alembic(db, "upgrade", "head")
        await _verify_schema(db, expect_seeded=True)

        # повторный upgrade — no-op
        _alembic(db, "upgrade", "head")
        await _verify_schema(db, expect_seeded=True)
    finally:
        admin = await asyncpg.connect(dsn=_dsn("postgres"))
        try:
            await admin.execute(
                "SELECT pg_terminate_backend(pid) FROM pg_stat_activity"
                f" WHERE datname = '{db}' AND pid <> pg_backend_pid()"
            )
            await admin.execute(f'DROP DATABASE IF EXISTS "{db}"')
        finally:
            await admin.close()


def test_migrations_full_cycle() -> None:
    asyncio.run(_lifecycle())
