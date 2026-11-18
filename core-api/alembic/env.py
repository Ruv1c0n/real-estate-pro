"""Alembic Environment для схемы core.

URL подключения берётся из app.config.Settings: DATABASE_URL целиком либо
сборка из POSTGRES_* (как у compose и CI). В alembic.ini секретов нет.

Модели SQLAlchemy появятся позже (зона Тимура) — до их появления миграции
пишутся вручную: target_metadata остаётся None, autogenerate не используется.
Таблица версий alembic_version — в public (см. do_run_migrations).
"""

import asyncio
from logging.config import fileConfig

from alembic import context
from app.config import settings
from sqlalchemy import pool
from sqlalchemy.engine import Connection
from sqlalchemy.ext.asyncio import async_engine_from_config

config = context.config

if config.config_file_name is not None:
    fileConfig(config.config_file_name, disable_existing_loggers=False)

# URL подставляем программно — alembic.ini хранится без секретов.
# Знаки % экранируются (%%): configparser валидирует синтаксис интерполяции
# уже в set_main_option и интерполирует %XX при чтении секции — пароль с
# percent-encoding (например, '@' -> %40) уронил бы миграции ещё до их
# запуска (ревью Даниила, PR #81; та же история, что и с паролями в
# POSTGRES_* на #39).
config.set_main_option("sqlalchemy.url", settings.database_url.replace("%", "%%"))

target_metadata = None


def run_migrations_offline() -> None:
    """Генерация SQL без подключения (--sql)."""
    context.configure(
        url=settings.database_url,
        target_metadata=target_metadata,
        literal_binds=True,
        dialect_opts={"paramstyle": "named"},
    )
    with context.begin_transaction():
        context.run_migrations()


def do_run_migrations(connection: Connection) -> None:
    # ВАЖНО: без SQL-запросов до context.configure — любой запрос здесь
    # открывает транзакцию раньше alembic, и тогда alembic завершает
    # миграцию ROLLBACK'ом вместо COMMIT'а.
    #
    # alembic_version сознательно живёт в public: alembic создаёт таблицу
    # версий ДО запуска первой миграции, а схему core создаёт сама
    # миграция 0001 — иначе на пустой БД был бы deadlock. Состав схемы
    # core при этом меняется только миграциями (НФТ-6).
    context.configure(
        connection=connection,
        target_metadata=target_metadata,
        compare_type=True,
    )
    with context.begin_transaction():
        context.run_migrations()


async def run_async_migrations() -> None:
    connectable = async_engine_from_config(
        config.get_section(config.config_ini_section, {}),
        prefix="sqlalchemy.",
        poolclass=pool.NullPool,
    )
    async with connectable.connect() as connection:
        await connection.run_sync(do_run_migrations)
    await connectable.dispose()


def run_migrations_online() -> None:
    asyncio.run(run_async_migrations())


if context.is_offline_mode():
    run_migrations_offline()
else:
    run_migrations_online()
