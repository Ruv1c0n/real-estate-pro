#!/usr/bin/env bash
# Проверка восстановления дампа (ТЗ v1.1, 6.1): восстанавливает дамп в
# одноразовый Postgres-контейнер и сверяет число строк в каждой таблице
# с манифестом, записанным рядом с дампом scripts/db/backup.sh.
#
# Проверяется «дамп действительно восстанавливается», а не «файл на месте»:
# расхождение набора таблиц или числа строк — FAIL.
#
# Использование (из корня репо):
#   scripts/db/restore-verify.sh backups/realestate-YYYYmmdd-HHMMSS.dump
#
# Код возврата: 0 — PASS; 1 — FAIL (нет файлов, ошибка восстановления,
# расхождение строк). Контейнер удаляется скриптом в любом случае.
# Переменные: VERIFY_PORT (55433), PG_IMAGE (postgres:16),
# PG_USER (app), PG_DB (realestate).

set -euo pipefail

DUMP_FILE="${1:?Использование: restore-verify.sh <dump.dump>}"
MANIFEST_FILE="${DUMP_FILE%.dump}.manifest.tsv"
VERIFY_PORT="${VERIFY_PORT:-55433}"
PG_IMAGE="${PG_IMAGE:-postgres:16}"
PG_USER="${PG_USER:-app}"
PG_DB="${PG_DB:-realestate}"

if [ ! -f "$DUMP_FILE" ]; then
    echo "Нет дампа: $DUMP_FILE" >&2
    exit 1
fi
if [ ! -f "$MANIFEST_FILE" ]; then
    echo "Нет манифеста: $MANIFEST_FILE (создаётся scripts/db/backup.sh вместе с дампом)" >&2
    exit 1
fi

CONTAINER="restore-verify-$$"
COMPUTED=$(mktemp)
# Функция вызывается непрямым путём: trap cleanup EXIT ниже.
# shellcheck disable=SC2329
cleanup() {
    docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
    rm -f "$COMPUTED"
}
trap cleanup EXIT

echo "Одноразовый $PG_IMAGE на 127.0.0.1:$VERIFY_PORT (контейнер $CONTAINER)"
docker run -d --name "$CONTAINER" \
    -e POSTGRES_USER="$PG_USER" \
    -e POSTGRES_PASSWORD=verify \
    -e POSTGRES_DB="$PG_DB" \
    -p "127.0.0.1:$VERIFY_PORT:5432" \
    "$PG_IMAGE" >/dev/null

READY=0
for _ in $(seq 1 60); do
    if docker exec "$CONTAINER" pg_isready -U "$PG_USER" -d "$PG_DB" >/dev/null 2>&1; then
        READY=1
        break
    fi
    sleep 1
done
if [ "$READY" -ne 1 ]; then
    echo "FAIL: контейнер не поднялся за 60 с" >&2
    exit 1
fi

echo "Восстановление дампа..."
# --exit-on-error: частично битый дамп сразу FAIL с сырой ошибкой pg_restore,
# а не «восстановилось всё, кроме куска» (ревью Даниила, PR #83)
docker exec -i "$CONTAINER" pg_restore -U "$PG_USER" -d "$PG_DB" \
    --exit-on-error --no-owner --no-privileges < "$DUMP_FILE"

# Пересчёт строк в восстановленной копии — тот же двухшаговый запрос,
# что и в backup.sh, поэтому сверяются одинаковые множества таблиц
COUNTS_Q=$(docker exec "$CONTAINER" psql -U "$PG_USER" -d "$PG_DB" -At -c "
SELECT COALESCE(string_agg(
    format('SELECT %L AS tbl, count(*) AS n FROM %I.%I', s || '.' || t, s, t),
    E'\nUNION ALL\n' ORDER BY s, t),
    'SELECT NULL AS tbl, NULL AS n WHERE false')
FROM (
    SELECT nspname AS s, relname AS t
    FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE c.relkind = 'r'
      AND n.nspname NOT IN ('pg_catalog', 'information_schema')
) t;")

docker exec "$CONTAINER" psql -U "$PG_USER" -d "$PG_DB" -At -F '|' -c "$COUNTS_Q" \
    | sort > "$COMPUTED"

if cmp -s <(sort "$MANIFEST_FILE") "$COMPUTED"; then
    echo "PASS: дамп восстановился, все $(wc -l < "$MANIFEST_FILE") таблиц совпали по числу строк"
    exit 0
else
    echo "FAIL: расхождение источник-манифест (left) против восстановленной копии (right):" >&2
    diff <(sort "$MANIFEST_FILE") "$COMPUTED" | sed 's/^/  /' >&2 || true
    exit 1
fi
