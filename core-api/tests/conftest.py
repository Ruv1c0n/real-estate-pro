import asyncio
import os
import subprocess
import sys
from contextlib import asynccontextmanager
from pathlib import Path
from uuid import uuid4

import asyncpg
import pytest
from sqlalchemy.engine import make_url

CORE_API_DIR = Path(__file__).resolve().parents[1]


@pytest.fixture(scope="module")
def temporary_database():
    admin_dsn = os.environ.get("MIGRATION_TEST_DSN")
    if not admin_dsn:
        pytest.skip("Для тестов PostgreSQL нужен MIGRATION_TEST_DSN")

    @asynccontextmanager
    async def create():
        name = f"core_test_{uuid4().hex}"
        url = make_url(admin_dsn).set(database=name)
        admin = await asyncpg.connect(admin_dsn)
        try:
            await admin.execute(f'CREATE DATABASE "{name}"')
            try:
                yield url
            finally:
                await admin.execute(f'DROP DATABASE IF EXISTS "{name}" WITH (FORCE)')
        finally:
            await admin.close()

    return create


@pytest.fixture(scope="module")
def run_alembic():
    async def run(url, *args):
        app_url = url.set(drivername="postgresql+asyncpg")
        result = await asyncio.to_thread(
            subprocess.run,
            [sys.executable, "-m", "alembic", "-c", "alembic.ini", *args],
            cwd=CORE_API_DIR,
            env={
                **os.environ,
                "DATABASE_URL": app_url.render_as_string(hide_password=False),
            },
            capture_output=True,
            text=True,
            timeout=180,
            check=False,
        )
        assert result.returncode == 0, (
            f"alembic {' '.join(args)} упал:\n{result.stdout}\n{result.stderr}"
        )
        return result.stdout

    return run
