"""DDL-права роли notifier и владение объектами схемы notifier (ТЗ 6.1, 6.5).

Issue #135. В 0002 роль notifier получила только DML (SELECT/INSERT/UPDATE/
DELETE) — встроенный раннер notifier при старте выполняет SQL-миграции
(CREATE TABLE IF NOT EXISTS, ALTER TABLE) и под сервисной ролью падал с
permission denied / must be owner. Пары NOTIFIER_DB_USER/NOTIFIER_DB_PASSWORD
были заблокированы как известное ограничение (тело #91, .env.example).

Что делает миграция:

1. GRANT CREATE ON SCHEMA notifier TO notifier — DDL своей схемы. CREATE на
   уровне базы и владение самой схемой не выдаются (шире пределов ТЗ 6.5):
   роль notifier продолжает видеть только свою схему.
2. Владение существующими объектами схемы notifier переносится к notifier
   (users, subscriptions, notifications_log, служебная schema_migrations
   раннера, identity-последовательности) — иначе ALTER-миграции 0002/0003
   под ролью notifier падают с must be owner. Перенос условный: на чистой БД
   таблиц notifier ещё нет (их создаст раннер уже под ролью notifier), поэтому
   фильтр по владельцу, а не перечисление имён. Схема остаётся во владении
   bootstrap-роли (см. проверку в тесте миграций).
3. Дефолтные права с ЯВНЫМ FOR ROLE app: гранты не зависят от того, под кем
   прогнали Alembic (в 0002 FOR ROLE не указан и привязан к роли запуска).
   Симметричная сторона FOR ROLE notifier -> app: объекты, созданные ролью
   notifier в per-service режиме, остаются доступны общей роли — возврат
   из per-service в дефолтный режим не ломает сервис.
4. Бывший владелец теряет владельческие права при переносе — компенсируем
   прямыми GRANT DML каждому прежнему владельцу ДО ALTER OWNER (владелец
   выдаёт права на свои объекты без superuser, ACL переживает смену
   владельца). Дефолтный режим «общая роль app» не затронут.
5. core не трогается: роль core_api остаётся DML-only, миграции core всегда
   под общей ролью.

Идемпотентность: GRANT и ALTER OWNER ... OWNER TO (тот же владелец)
повторно безопасны; перенос фильтруется по текущему владельцу.

Downgrade обязан выполниться ДО DROP ROLE notifier в downgrade 0002:
роль, владеющая объектами, не удаляется. Поэтому обратный перенос владения
к прежнему владельцу идёт первой же командой downgrade.

Предполагается запуск от bootstrap-роли проекта (app — POSTGRES_USER
compose/CI): superuser может задавать дефолтные права FOR ROLE notifier
и ALTER OWNER для любых объектов.

Revision ID: 0004
Revises: 0003
Create Date: 2026-11-27
"""

from collections.abc import Sequence

from alembic import op

# revision identifiers, used by Alembic.
revision: str = "0004"
down_revision: str | None = "0003"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None

# Перенос владения объектами схемы notifier указанному целевому владельцу.
# Компенсация прежнему владельцу выдаётся ДО ALTER OWNER (см. п.4 докстринга).
# В схеме notifier используются только обычные таблицы (relkind 'r') и
# identity-последовательности ('S') — иные типы объектов при появлении
# потребуют расширения DO-блока. Последовательности, привязанные к колонкам
# (serial/identity, pg_depend deptype 'a'/'i'), в цикле последовательностей
# не участвуют: их владельца меняет сам ALTER TABLE, а прямой
# ALTER SEQUENCE ... OWNER для них запрещён сервером.
_TRANSFER_OWNERSHIP = """
DO $$
DECLARE
    obj RECORD;
BEGIN
    FOR obj IN
        SELECT c.oid::regclass AS rel,
               c.relkind AS relkind,
               c.relowner::regrole::text AS old_owner
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'notifier'
          AND c.relkind IN ('r', 'S')
          AND c.relowner <> '{target}'::regrole
          AND NOT (
              c.relkind = 'S' AND EXISTS (
                  SELECT 1 FROM pg_depend d
                  WHERE d.objid = c.oid
                    AND d.classid = 'pg_class'::regclass
                    AND d.refclassid = 'pg_class'::regclass
                    AND d.deptype IN ('a', 'i'))
          )
        ORDER BY c.relkind, c.relname
    LOOP
        IF obj.relkind = 'S' THEN
            EXECUTE format(
                'GRANT USAGE, SELECT ON SEQUENCE %s TO %I', obj.rel, obj.old_owner);
            EXECUTE format('ALTER SEQUENCE %s OWNER TO {target}', obj.rel);
        ELSE
            EXECUTE format(
                'GRANT SELECT, INSERT, UPDATE, DELETE ON %s TO %I',
                obj.rel, obj.old_owner);
            EXECUTE format('ALTER TABLE %s OWNER TO {target}', obj.rel);
        END IF;
    END LOOP;
END
$$;
"""


def upgrade() -> None:
    # П.1: DDL своей схемы (CREATE на уровне БД и владение схемой не выдаются)
    op.execute("GRANT CREATE ON SCHEMA notifier TO notifier")

    # П.2: перенос владения существующих объектов схемы notifier к роли
    # notifier (п.4 внутри — компенсация прежним владельцам)
    op.execute(_TRANSFER_OWNERSHIP.format(target="notifier"))

    # П.3: дефолтные права с явным FOR ROLE — независимо от роли запуска
    # Alembic; обе стороны зеркалят DML-гранты 0002
    op.execute(
        "ALTER DEFAULT PRIVILEGES FOR ROLE app IN SCHEMA notifier"
        " GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO notifier"
    )
    op.execute(
        "ALTER DEFAULT PRIVILEGES FOR ROLE app IN SCHEMA notifier"
        " GRANT USAGE, SELECT ON SEQUENCES TO notifier"
    )
    op.execute(
        "ALTER DEFAULT PRIVILEGES FOR ROLE notifier IN SCHEMA notifier"
        " GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO app"
    )
    op.execute(
        "ALTER DEFAULT PRIVILEGES FOR ROLE notifier IN SCHEMA notifier"
        " GRANT USAGE, SELECT ON SEQUENCES TO app"
    )


def downgrade() -> None:
    # Сначала владение — DROP ROLE notifier в downgrade 0002 иначе упадёт
    op.execute(_TRANSFER_OWNERSHIP.format(target="app"))

    # Отзыв дефолтных прав в обе стороны (зеркально upgrade)
    op.execute(
        "ALTER DEFAULT PRIVILEGES FOR ROLE app IN SCHEMA notifier"
        " REVOKE ALL ON TABLES FROM notifier"
    )
    op.execute(
        "ALTER DEFAULT PRIVILEGES FOR ROLE app IN SCHEMA notifier"
        " REVOKE ALL ON SEQUENCES FROM notifier"
    )
    op.execute(
        "ALTER DEFAULT PRIVILEGES FOR ROLE notifier IN SCHEMA notifier"
        " REVOKE ALL ON TABLES FROM app"
    )
    op.execute(
        "ALTER DEFAULT PRIVILEGES FOR ROLE notifier IN SCHEMA notifier"
        " REVOKE ALL ON SEQUENCES FROM app"
    )

    # Компенсационные GRANT'ы прежнему владельцу больше не нужны:
    # владение возвращено, владельческие права восстановлены
    op.execute(
        "REVOKE SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA notifier FROM app"
    )
    op.execute("REVOKE USAGE, SELECT ON ALL SEQUENCES IN SCHEMA notifier FROM app")

    # П.1: DDL-право роли notifier отзывается
    op.execute("REVOKE CREATE ON SCHEMA notifier FROM notifier")
