#!/usr/bin/env bash
# restore.sh — восстановление из дампа backup.sh на ЧИСТУЮ БД (ТЗ 6.1:
# «проверка восстановления»). Опасный скрипт: по умолчанию восстанавливает
# в отдельную БД realestate_restore_check, а не в рабочую.
#
# Вход:  файл дампа первым аргументом;
#        RESTORE_DB (default realestate_restore_check),
#        POSTGRES_CONTAINER/USER как в backup.sh.
# Проверка восстановления: восстановили → сравнили количество записей
# ключевых таблиц с рабочей БД (см. docs/qa/backup-runbook.md).

set -euo pipefail

dump="${1:?Укажи файл дампа: scripts/ops/restore.sh backups/realestate-....sql.gz}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-real-estate-pro-postgres-1}"
POSTGRES_USER="${POSTGRES_USER:-app}"
RESTORE_DB="${RESTORE_DB:-realestate_restore_check}"

if [ ! -f "$dump" ]; then
    echo "Файл дампа не найден: $dump" >&2
    exit 1
fi

# БД для проверки создаём заново каждый раз (drop/create).
docker exec "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d postgres -qAt \
    -c "DROP DATABASE IF EXISTS $RESTORE_DB" > /dev/null
docker exec "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d postgres -qAt \
    -c "CREATE DATABASE $RESTORE_DB" > /dev/null

gunzip -c "$dump" | docker exec -i "$POSTGRES_CONTAINER" \
    psql -U "$POSTGRES_USER" -d "$RESTORE_DB" -v ON_ERROR_STOP=1 -q

echo "Восстановлено в $RESTORE_DB:"
for t in notifier.users notifier.subscriptions notifier.notifications_log; do
    n=$(docker exec "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d "$RESTORE_DB" -qAt \
        -c "SELECT count(*) FROM $t" 2>/dev/null || echo "?")
    echo "  $t: $n"
done
echo "Сверь с рабочей БД (docker exec $POSTGRES_CONTAINER psql -U $POSTGRES_USER -d realestate -c 'SELECT count(*) FROM notifier.users')."
echo "Проверку выполнил? Удаляй: DROP DATABASE $RESTORE_DB"
