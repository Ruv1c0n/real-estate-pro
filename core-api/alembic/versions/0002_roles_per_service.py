"""Роли и права per-service (ТЗ 6.1, 6.5; правило AGENTS.md 6.5 п.2).

core_api  — полный доступ только к схеме core;
notifier  — полный доступ только к схеме notifier;
коллектор в БД не ходит (только HTTP к Core API), роли для него нет.

Каждый сервис видит только свою схему — чужая закрыта и через отсутствие
GRANT'ов (PUBLIC не получает USAGE на непубличные схемы), и через
отсутствие выдачи роли.

Пароли в миграциях НЕ задаются (секреты — только .env, правило 5): роли
создаются LOGIN без пароля, поэтому до явного `ALTER ROLE ... PASSWORD`
при деплое подключение под сервисной ролью невозможно. Владелец объектов
и runner миграций остаётся `app`.

Идемпотентность: роли и схема notifier через IF NOT EXISTS — повторный
прогон безопасен; на чистой БД alembic 0002 может примениться раньше
SQL-миграций notifier (их запускает runner Даниила при старте notifier),
поэтому схема notifier гарантированно создаётся здесь же.

Revision ID: 0002
Revises: 0001
Create Date: 2026-11-17
"""

from collections.abc import Sequence

from alembic import op

# revision identifiers, used by Alembic.
revision: str = "0002"
down_revision: str | None = "0001"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    # Роли LOGIN без пароля: пароль задаётся при депое (ALTER ROLE ... PASSWORD,
    # значение из .env), в миграциях секретов быть не должно
    op.execute(
        """
        DO $$
        BEGIN
            IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'core_api') THEN
                CREATE ROLE core_api LOGIN;
            END IF;
            IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'notifier') THEN
                CREATE ROLE notifier LOGIN;
            END IF;
        END
        $$;
        """
    )

    # На чистой БД схема notifier может ещё не существовать (SQL-миграции
    # notifier применяются runner'ом notifier); миграции 0001 создаёт её
    # с IF NOT EXISTS, здесь гарантируем то же самое
    op.execute("CREATE SCHEMA IF NOT EXISTS notifier")

    # --- core_api: только схема core ---
    op.execute("GRANT USAGE ON SCHEMA core TO core_api")
    op.execute(
        "GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA core TO core_api"
    )
    # identity-колонкам для INSERT нужны права на последовательности
    op.execute("GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA core TO core_api")
    # будущие объекты создают миграции от имени app — закрываем дефолтными правами
    op.execute(
        "ALTER DEFAULT PRIVILEGES IN SCHEMA core"
        " GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO core_api"
    )
    op.execute(
        "ALTER DEFAULT PRIVILEGES IN SCHEMA core GRANT USAGE, SELECT ON SEQUENCES TO core_api"
    )

    # --- notifier: только схема notifier; доступ к core не выдаётся (ТЗ 6.5) ---
    op.execute("GRANT USAGE ON SCHEMA notifier TO notifier")
    op.execute(
        "GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA notifier TO notifier"
    )
    op.execute("GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA notifier TO notifier")
    op.execute(
        "ALTER DEFAULT PRIVILEGES IN SCHEMA notifier"
        " GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO notifier"
    )
    op.execute(
        "ALTER DEFAULT PRIVILEGES IN SCHEMA notifier GRANT USAGE, SELECT ON SEQUENCES TO notifier"
    )


def downgrade() -> None:
    # Отзываем то, что выдали, включая дефолтные права, затем удаляем роли.
    # Роли без паролей и без созданных ими объектов — DROP безопасен.
    op.execute(
        "ALTER DEFAULT PRIVILEGES IN SCHEMA core REVOKE ALL ON TABLES FROM core_api"
    )
    op.execute(
        "ALTER DEFAULT PRIVILEGES IN SCHEMA core REVOKE ALL ON SEQUENCES FROM core_api"
    )
    op.execute(
        "REVOKE SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA core FROM core_api"
    )
    op.execute("REVOKE USAGE, SELECT ON ALL SEQUENCES IN SCHEMA core FROM core_api")
    op.execute("REVOKE USAGE ON SCHEMA core FROM core_api")

    op.execute(
        "ALTER DEFAULT PRIVILEGES IN SCHEMA notifier REVOKE ALL ON TABLES FROM notifier"
    )
    op.execute(
        "ALTER DEFAULT PRIVILEGES IN SCHEMA notifier REVOKE ALL ON SEQUENCES FROM notifier"
    )
    op.execute(
        "REVOKE SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA notifier FROM notifier"
    )
    op.execute("REVOKE USAGE, SELECT ON ALL SEQUENCES IN SCHEMA notifier FROM notifier")
    op.execute("REVOKE USAGE ON SCHEMA notifier FROM notifier")

    op.execute("DROP ROLE IF EXISTS core_api")
    op.execute("DROP ROLE IF EXISTS notifier")
