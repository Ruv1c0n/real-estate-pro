#!/usr/bin/env bash
# Резервное копирование PostgreSQL проекта в custom-формат pg_dump (-Fc).
# ТЗ v1.1, раздел 6.1: резервное копирование и проверка восстановления.
#
# Вместе с дампом создаётся манифест (tbl|rows) — по нему
# scripts/db/restore-verify.sh проверяет, что дамп восстанавливается
# полностью, а не «файл на месте, внутри пусто».
#
# Использование (из корня репо):
#   scripts/db/backup.sh                     # backups/realestate-YYYYmmdd-HHMMSS.dump
#   scripts/db/backup.sh path/to/dump.dump   # произвольный путь
#
# Источник данных, MODE=compose (по умолчанию) — сервис `postgres` из
# docker-compose.yml. MODE=host — произвольный Postgres, доступный с хоста:
#   PG_MODE=host PG_HOST=host.docker.internal PG_PORT=5433 scripts/db/backup.sh
#
# Все переменные читаются из окружения (по умолчанию — значения .env.example):
#   PG_USER (app), PG_PASSWORD (app), PG_DB (realestate),
#   PG_MODE (compose|host), PG_HOST, PG_PORT, PG_IMAGE (postgres:16),
#   BACKUP_DIR (backups), COMPOSE_DIR (.)
#
# Пароль нигде не сохраняется: он передаётся только в переменные окружения
# разового контейнера (секреты — только .env, правило AGENTS.md 5).

set -euo pipefail

PG_MODE="${PG_MODE:-compose}"
PG_USER="${PG_USER:-app}"
PG_PASSWORD="${PG_PASSWORD:-app}"
PG_DB="${PG_DB:-realestate}"
PG_HOST="${PG_HOST:-host.docker.internal}"
PG_PORT="${PG_PORT:-5432}"
PG_IMAGE="${PG_IMAGE:-postgres:16}"
BACKUP_DIR="${BACKUP_DIR:-backups}"
COMPOSE_DIR="${COMPOSE_DIR:-.}"

DUMP_FILE="${1:-$BACKUP_DIR/realestate-$(date +%Y%m%d-%H%M%S).dump}"
MANIFEST_FILE="${DUMP_FILE%.dump}.manifest.tsv"

mkdir -p "$(dirname "$DUMP_FILE")"

COMPOSE=(docker compose -f "$COMPOSE_DIR/docker-compose.yml" exec -T postgres)

if [ "$PG_MODE" = "compose" ]; then
    if [ -z "$(docker compose -f "$COMPOSE_DIR/docker-compose.yml" ps -q postgres)" ]; then
        echo "Сервис postgres не запущен: docker compose up -d postgres" >&2
        exit 1
    fi
    PG=("${COMPOSE[@]}")
    HOST_ARGS=()
else
    PG=(docker run --rm -i -e "PGPASSWORD=$PG_PASSWORD" "$PG_IMAGE")
    HOST_ARGS=(-h "$PG_HOST" -p "$PG_PORT")
fi

echo "Дамп $PG_DB из MODE=$PG_MODE -> $DUMP_FILE"
"${PG[@]}" pg_dump -U "$PG_USER" -d "$PG_DB" -Fc "${HOST_ARGS[@]}" > "$DUMP_FILE"

# Манифест: по каждому row-чанку count(*). Двухшагово: сначала генерируем
# UNION-запрос по всем обычным таблицам, затем выполняем его. Системные
# схемы исключены; alembic_version (public) в проверку попадает.
COUNTS_Q=$("${PG[@]}" psql -U "$PG_USER" -d "$PG_DB" -At "${HOST_ARGS[@]}" -c "
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

"${PG[@]}" psql -U "$PG_USER" -d "$PG_DB" -At -F '|' "${HOST_ARGS[@]}" -c "$COUNTS_Q" \
    > "$MANIFEST_FILE"

DUMP_SIZE=$(wc -c < "$DUMP_FILE")
TABLES=$(wc -l < "$MANIFEST_FILE")
echo "Готово: $DUMP_FILE ($DUMP_SIZE байт), таблиц в манифесте: $TABLES"
echo "Манифест: $MANIFEST_FILE"
echo "Проверка восстановления: scripts/db/restore-verify.sh $DUMP_FILE"
