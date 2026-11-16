#!/usr/bin/env bash
# backup.sh — снимок PostgreSQL стенда (ТЗ 6.1: резервное копирование и
# проверка восстановления — зона Ильи; механика запуска — compose/ops, Даниил).
#
# Снимает pg_dump обеих схем (core, notifier) в timestamped-файл.
# Не поднимает и не гасит стенд — работает с живым контейнером postgres.
#
# Вход:  BACKUP_DIR (default ./backups, в git не попадает — добавь в .gitignore
#        или держи вне репо), POSTGRES_CONTAINER (default по compose: ищем
#        контейнер postgres), POSTGRES_USER/POSTGRES_DB (defaults app/realestate).
# Выход: путь к файлу дампа на stdout; код 0 при успехе.

set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-./backups}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-real-estate-pro-postgres-1}"
POSTGRES_USER="${POSTGRES_USER:-app}"
POSTGRES_DB="${POSTGRES_DB:-realestate}"

if ! docker inspect "$POSTGRES_CONTAINER" > /dev/null 2>&1; then
    echo "Контейнер $POSTGRES_CONTAINER не найден. Стенд поднят? (docker compose up -d postgres)" >&2
    exit 1
fi

mkdir -p "$BACKUP_DIR"
ts=$(date +%Y%m%d-%H%M%S)
out="$BACKUP_DIR/realestate-$ts.sql.gz"

docker exec "$POSTGRES_CONTAINER" \
    pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --schemas=core,notifier \
    | gzip > "$out"

size=$(du -h "$out" | cut -f1)
echo "$out"
echo "Размер: $size. Проверка восстановления — см. docs/qa/backup-runbook.md (restore.sh)."
