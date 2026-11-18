#!/usr/bin/env bash
# pgbench-run.sh — каркас анализа запросов (ТЗ 6.1, зона Ильи — анализ;
# механика прогона — Даниил). Кастомный pgbench по таблице listings:
# проверяет поведение индексов под типовые фильтры ФТ-2.2.
#
# ПРЕДУСЛОВИЯ: таблица listings с данными (появится с core-миграциями +
# дампом 500+). Скрипт честно выходит, если её нет — каркас, не приёмка.
#
# Вход: POSTGRES_CONTAINER/USER/DB как в scripts/db/backup.sh (PR #83);
# T (секунд, default 30);
# C (клиентов, default 1 — НФТ-10 меряет «одного последовательного клиента»).

set -euo pipefail

POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-real-estate-pro-postgres-1}"
POSTGRES_USER="${POSTGRES_USER:-app}"
POSTGRES_DB="${POSTGRES_DB:-realestate}"
T="${T:-30}"
C="${C:-1}"

has_listings=$(docker exec "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt \
    -c "SELECT to_regclass('core.listings') IS NOT NULL" 2>/dev/null || echo "f")
if [ "$has_listings" != "t" ]; then
    echo "core.listings не существует — каркас ждёт core-миграции и дамп (issue критического пути)." >&2
    exit 2
fi

n=$(docker exec "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt \
    -c "SELECT count(*) FROM core.listings")
echo "Данных в core.listings: $n (по ТЗ 3.5/НФТ-10 приёмка — на 500+)"

# Типовые запросы ФТ-2.2/ТЗ 4.2 — три индекса из ТЗ 4.2 задействуются по очереди.
cat > /tmp/pgbench_select.sql <<'SQL'
\set city_id 1
\set rooms 2
\set maxp 500
SELECT id FROM core.listings
 WHERE deal_type = 'rent' AND city_id = :city_id AND price_usd <= :maxp
 ORDER BY price_usd LIMIT 20;
SELECT id FROM core.listings
 WHERE deal_type = 'rent' AND city_id = :city_id AND rooms = :rooms
 ORDER BY created_at DESC LIMIT 20;
SQL

docker cp /tmp/pgbench_select.sql "$POSTGRES_CONTAINER":/tmp/pgbench_select.sql
docker exec "$POSTGRES_CONTAINER" pgbench -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
    -f /tmp/pgbench_select.sql -T "$T" -c "$C" -n
echo "Отчёт для Ильи: compare планы по EXPLAIN (ANALYZE, BUFFERS) каждого запроса — pgbench даёт TPS/латентность, план — причина."
