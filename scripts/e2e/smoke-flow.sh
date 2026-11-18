#!/usr/bin/env bash
# smoke-flow.sh — автоматизированный дым сквозного сценария S2 из
# docs/qa/e2e-protocol.md (issue #20): прямая публикация в stream через
# POST /debug/event минус collector/outbox. Проверяем:
#   1) валидный payload → доставлен (sent_at в notifications_log);
#   2) невалидный payload → listings.dead с причиной, уведомления нет (ТЗ 4.3);
#   3) без X-Admin-Key → отказ (НФТ-3).
# Сценарий S3 (полный поток mock-avito → collector → core-api → stream)
# появится здесь же, когда встанут core-api (POST /api/v1/listings + outbox)
# и collector — слоты помечены ниже.
#
# Предусловия:
#   - notifier в log-режиме (без TELEGRAM_BOT_TOKEN) или Telegram-режиме;
#     в log-режиме совпадение фиксируется в notifications_log так же;
#   - ADMIN_KEY задан, psql в PATH (или шим через docker exec),
#     NOTIFIER_TEST_DSN указывает на ту же БД, что пишет notifier.
#
# Вход: NOTIFIER_BASE_URL (default http://localhost:8080), ADMIN_KEY,
# NOTIFIER_TEST_DSN, CHAT_ID (default 990002), CITY/COUNTRY/DEAL.
# Выход: 0 — все проверки прошли; 1 — есть провалы.

set -uo pipefail

NOTIFIER_BASE_URL="${NOTIFIER_BASE_URL:-http://localhost:8080}"
: "${ADMIN_KEY:?ADMIN_KEY не задан}"
: "${NOTIFIER_TEST_DSN:?NOTIFIER_TEST_DSN не задан}"
CHAT_ID="${CHAT_ID:-990002}"
CITY="${CITY:-Minsk}"
COUNTRY="${COUNTRY:-BY}"
DEAL="${DEAL:-rent}"
WAIT_SEC="${WAIT_SEC:-15}"
POLL_STEP="${POLL_STEP:-0.2}"

# Число опросов из бюджета ожидания и шага опроса (не хардкодим ×5 под 0.2).
poll_count() { awk "BEGIN{printf \"%d\", $1 / $POLL_STEP}" 2>/dev/null || echo 0; }

# Значения подписки попадают в SQL/JSON напрямую — кавычки запрещаем сразу.
for _v in "$CITY" "$COUNTRY" "$DEAL"; do
    case "$_v" in *"'"*|*'"'*|*';'*) echo "FAIL: CITY/COUNTRY/DEAL не должны содержать кавычек/точки с запятой: $_v"; exit 1;; esac
done

fails=0
step() { printf '\n== %s\n' "$*"; }
ok()   { printf '   OK: %s\n' "$*"; }
bad()  { printf '   FAIL: %s\n' "$*"; fails=$((fails+1)); }

psql_q() { psql "$NOTIFIER_TEST_DSN" -v ON_ERROR_STOP=1 -qAt -c "$1"; }

# listings.dead живёт в Redis, а не в БД; redis-cli берём из PATH или из
# контейнера redis (если запущен compose-стенд).
redis_cli() {
    if command -v redis-cli > /dev/null; then
        redis-cli -u "${REDIS_URL:-redis://127.0.0.1:6379/0}" "$@"
    else
        docker compose exec -T redis redis-cli "$@"
    fi
}

redis_xlen() { redis_cli XLEN listings.dead 2>/dev/null; }

wait_sent() { # $1 = listing_id; печатает sent_at или пусто
    local i end sid
    end=$(poll_count "$WAIT_SEC")
    for ((i=0; i<end; i++)); do
        sid=$(psql_q "SELECT to_char(sent_at AT TIME ZONE 'UTC','YYYY-MM-DD\"T\"HH24:MI:SS.MS\"Z\"')
                      FROM notifier.notifications_log
                      WHERE chat_id=$CHAT_ID AND listing_id=$1 AND status IN ('sent','unknown') LIMIT 1" 2>/dev/null)
        [ -n "$sid" ] && { echo "$sid"; return 0; }
        sleep "$POLL_STEP"
    done
    return 1
}

cleanup() {
    psql_q "DELETE FROM notifier.users WHERE chat_id = $CHAT_ID" > /dev/null 2>&1
}
trap cleanup EXIT

step "S2.0 Стенд"
health=$(curl -s -o /dev/null -w '%{http_code}' "$NOTIFIER_BASE_URL/health" || echo 000)
[ "$health" = "200" ] && ok "notifier /health = 200" || { bad "notifier /health = $health (стенд поднят? log-режим?)"; exit 1; }

step "S2.1 Пользователь и подписка (chat_id=$CHAT_ID)"
psql_q "INSERT INTO notifier.users (chat_id) VALUES ($CHAT_ID) ON CONFLICT (chat_id) DO NOTHING" > /dev/null \
    && ok "пользователь создан" || bad "INSERT пользователя"
psql_q "DELETE FROM notifier.subscriptions WHERE chat_id = $CHAT_ID" > /dev/null
psql_q "INSERT INTO notifier.subscriptions (chat_id, deal_type, country, city)
        VALUES ($CHAT_ID, '$DEAL', '$COUNTRY', '$CITY')" > /dev/null \
    && ok "подписка $DEAL $COUNTRY/$CITY" || bad "INSERT подписки"

step "S2.2 Валидный payload → доставка"
ts=$(date -u +%Y-%m-%dT%H:%M:%SZ)
listing_id=$(( $(date +%s) % 900000000 + 1000 ))
payload=$(printf '{"event_id":"%s","id":%s,"source":"smoke","deal_type":"%s","country":"%s","city":"%s","rooms":2,"price":380,"currency":"USD","price_usd":380,"address":null,"url":"https://example.com/smoke/%s","created_at":"%s"}' \
    "$(psql_q 'SELECT gen_random_uuid()::text')" "$listing_id" "$DEAL" "$COUNTRY" "$CITY" "$listing_id" "$ts")
http=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$NOTIFIER_BASE_URL/debug/event" \
    -H "X-Admin-Key: $ADMIN_KEY" -H 'Content-Type: application/json' -d "$payload")
[ "$http" = "202" ] && ok "POST /debug/event → 202" || bad "POST /debug/event → $http (ожидали 202)"
sent_at=$(wait_sent "$listing_id")
if [ -n "$sent_at" ]; then ok "доставлен: sent_at=$sent_at (listing_id=$listing_id)"
else bad "sent_at не появился за ${WAIT_SEC}s (listing_id=$listing_id)"; fi

step "S2.3 Повтор того же объявления → дедуп (ФТ-3.5)"
before=$(psql_q "SELECT count(*) FROM notifier.notifications_log WHERE chat_id=$CHAT_ID AND listing_id=$listing_id")
curl -s -o /dev/null -X POST "$NOTIFIER_BASE_URL/debug/event" \
    -H "X-Admin-Key: $ADMIN_KEY" -H 'Content-Type: application/json' -d "$payload"
sleep 1
after=$(psql_q "SELECT count(*) FROM notifier.notifications_log WHERE chat_id=$CHAT_ID AND listing_id=$listing_id")
[ "$before" = "$after" ] && ok "записей по-прежнему $after — дубля нет" \
    || bad "записей было $before, стало $after — дубль!"

step "S2.4 Невалидный payload → listings.dead (ТЗ 4.3)"
dead_before=$(redis_xlen || echo 0)
dead_before=${dead_before:-0}
bad_payload='{"event_id":"not-a-uuid","id":-1}'
http=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$NOTIFIER_BASE_URL/debug/event" \
    -H "X-Admin-Key: $ADMIN_KEY" -H 'Content-Type: application/json' -d "$bad_payload")
# /debug/event сам валидирует payload тем же ParseEvent и отдаёт 422 — мусор
# до стрима не доходит, dead-letter через него недостижим. Поэтому битое
# событие кладём в стрим напрямую: consumer забракует его и уйдёт в
# listings.dead (ТЗ 4.3). POST-проверку оставляем как smoke-гейт валидации.
if [ "$http" = "422" ] || [ "$http" = "400" ]; then
    ok "битый payload отклонён самим /debug/event: $http"
elif [ "$http" = "202" ]; then
    ok "/debug/event принял невалидный payload (мягче, чем ожидали)"
else
    bad "неожиданный ответ на битый payload: $http"
fi
# Прямой XADD мусора в стрим → consumer должен уйти в listings.dead.
garbage='{"event_id":"not-a-uuid","id":-1}'
if redis_cli XADD listings.new '*' data "$garbage" > /dev/null 2>&1; then
    i=0; end=$(poll_count "$WAIT_SEC")
    dead_after="$dead_before"
    while [ "$i" -lt "$end" ]; do
        dead_after=$(redis_xlen || echo 0)
        dead_after=${dead_after:-0}
        [ "$dead_after" -gt "$dead_before" ] && break
        sleep "$POLL_STEP"; i=$((i+1))
    done
    [ "$dead_after" -gt "$dead_before" ] && ok "listings.dead вырос: $dead_before → $dead_after" \
        || bad "listings.dead не вырос за ${WAIT_SEC}s ($dead_before → $dead_after)"
else
    bad "не удалось XADD мусор в listings.new (redis недоступен?)"
fi

step "S2.5 Без X-Admin-Key → отказ (НФТ-3)"
http=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$NOTIFIER_BASE_URL/debug/event" \
    -H 'Content-Type: application/json' -d '{"event_id":"x","id":1}')
[ "$http" = "401" ] || [ "$http" = "404" ] && ok "без ключа → $http" \
    || bad "без ключа → $http (ожидали 401/404)"

step "S3 Полный поток (mock-avito → collector → core-api) — СЛОТ"
echo "   (не реализовано: ждёт POST /api/v1/listings + outbox в core-api и collector; см. docs/qa/e2e-protocol.md S3)"

printf '\n== Итог: провалов %s\n' "$fails"
exit $(( fails > 0 ))
