#!/usr/bin/env bash
# notifier-p95.sh — заготовка замера p95 доставки уведомлений для НФТ-11:
# «p95 от фиксации объявления до ответа Telegram < 5 с» при профиле нагрузки
# «<= 1 событие / 5 с», минимум 100 событий. Полный прогон по протоколу ТЗ
# (100 событий, чистый log-режим) — часть issue #20; этот скрипт — каркас
# замера, который оператор запускает вручную.
#
# Предусловия:
#   - notifier в log-режиме (без TELEGRAM_BOT_TOKEN): совпадения пишутся в
#     stdout, sent_at фиксируется в notifications_log (UTC);
#   - ADMIN_KEY задан (иначе POST /debug/event выключен, 404);
#   - psql в PATH и NOTIFIER_TEST_DSN указывает на ту же БД, что notifier.
#
# Вход: NOTIFIER_BASE_URL, ADMIN_KEY, NOTIFIER_TEST_DSN, N, INTERVAL.
# Шаги: создать пользователя и подписку, затем N раз: t0 -> POST /debug/event
# (валидный payload listings.new, уникальный listing id) -> опрос
# notifications_log по listing_id -> латентность sent_at - t0 (в мс).
#
# ВАЖНО: sent_at пишется на стороне БД (UTC), t0 снимается на машине
# оператора; расхождение часов входит в результат.

set -euo pipefail

NOTIFIER_BASE_URL="${NOTIFIER_BASE_URL:-http://localhost:8080}"
: "${ADMIN_KEY:?ADMIN_KEY не задан — POST /debug/event отвечает 404}"
: "${NOTIFIER_TEST_DSN:?NOTIFIER_TEST_DSN не задан — некуда писать и читать notifications_log}"

N="${N:-100}"
INTERVAL="${INTERVAL:-5s}"
CHAT_ID="${CHAT_ID:-990001}"
CITY="${CITY:-Minsk}"
COUNTRY="${COUNTRY:-BY}"
DEAL="${DEAL:-rent}"
# Бюджет ожидания sent_at для одного события в секундах (log-режим
# доставляет мгновенно; 15 с — с запасом против подвисшего consumer'а).
WAIT_SEC="${WAIT_SEC:-15}"
POLL_STEP="${POLL_STEP:-0.2}"

if ! [[ "$N" =~ ^[1-9][0-9]*$ ]]; then
    echo "N должен быть положительным целым, получено: $N" >&2
    exit 1
fi
WAIT_STEPS=$(( WAIT_SEC * 5 ))  # sleep "$POLL_STEP" -> 5 шагов на секунду

psql_q() {
    psql "$NOTIFIER_TEST_DSN" -v ON_ERROR_STOP=1 -qAt -c "$1"
}

# 1. Пользователь и подписка по схеме 0001 (chat_id тестовый).
# FK требует запись в users; подписка одна и перекрывает тестовый город.
psql_q "INSERT INTO notifier.users (chat_id) VALUES ($CHAT_ID) ON CONFLICT (chat_id) DO NOTHING" > /dev/null
psql_q "DELETE FROM notifier.subscriptions WHERE chat_id = $CHAT_ID" > /dev/null
psql_q "INSERT INTO notifier.subscriptions (chat_id, deal_type, country, city)
        VALUES ($CHAT_ID, '$DEAL', '$COUNTRY', '$CITY')" > /dev/null

echo "База: chat_id=$CHAT_ID, подписка city=$CITY; событий: $N, интервал: $INTERVAL"
latencies=()
delivered=0
missing=0

for i in $(seq 1 "$N"); do
    # Уникальный listing id (ms-метка + номер): UNIQUE (chat_id, listing_id)
    # дедупу не мешает — каждая публикация получает новую пару.
    listing_id=$(( $(date +%s) * 1000 + i ))
    event_id=$(psql_q "SELECT gen_random_uuid()::text")
    created_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    payload=$(printf '{"event_id":"%s","id":%s,"source":"mock","deal_type":"%s","country":"%s","city":"%s","rooms":1,"price":380,"currency":"USD","price_usd":380,"address":"Bench street %s","url":"https://example.com/%s","created_at":"%s"}' \
        "$event_id" "$listing_id" "$DEAL" "$COUNTRY" "$CITY" "$listing_id" "$listing_id" "$created_at")

    t0=$(date +%s%3N)  # фиксация объявления (НФТ-11: начало отсчёта)
    # || true: недоступный notifier даёт код 000 и попадает в лог ниже,
    # вместо обрыва всего прогона посреди замера.
    code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$NOTIFIER_BASE_URL/debug/event" \
        -H "X-Admin-Key: $ADMIN_KEY" -H "Content-Type: application/json" -d "$payload" || true)
    if [ "$code" != "202" ]; then
        echo "событие $i/$N: POST /debug/event вернул $code (нужен 202), пропускается" >&2
        sleep "$INTERVAL"
        continue
    fi

    # Опрос notifications_log по listing_id: sent_at пишется в UTC;
    # EXTRACT(EPOCH ...) даёт секунды, приводим к мс для сравнения с t0.
    sent_ms=""
    for _ in $(seq 1 "$WAIT_STEPS"); do
        sent_ms=$(psql_q "SELECT (EXTRACT(EPOCH FROM sent_at)*1000)::bigint
                          FROM notifier.notifications_log
                          WHERE chat_id = $CHAT_ID AND listing_id = $listing_id
                            AND sent_at IS NOT NULL LIMIT 1" || true)
        [ -n "$sent_ms" ] && break
        sleep "$POLL_STEP"
    done

    if [ -z "$sent_ms" ]; then
        echo "событие $i/$N: sent_at не появился за ${WAIT_SEC}s — событие пропускается" >&2
        missing=$((missing + 1))
    else
        latencies+=( $(( sent_ms - t0 )) )
        delivered=$((delivered + 1))
    fi

    sleep "$INTERVAL"  # профиль нагрузки: <= 1 событие / INTERVAL
done

echo
echo "Доставлено: $delivered из $N (не дождались sent_at: $missing)"
if [ "$delivered" -eq 0 ]; then
    echo "Нет ни одного замера — проверять p95 нечего." >&2
    exit 1
fi

echo "Латентности (мс, в порядке замера):"
printf '%s\n' "${latencies[@]}"

# p50/p95/max — метод ближайшего ранга: idx = ceil(k * N).
printf '%s\n' "${latencies[@]}" | sort -n | awk '
    function ceil(x) { return int(x) + (int(x) < x ? 1 : 0) }
    { a[NR] = $1 }
    END {
        p50 = a[ceil(0.50 * NR)]
        p95 = a[ceil(0.95 * NR)]
        printf "p50 = %d мс\n", p50
        printf "p95 = %d мс\n", p95
        printf "max = %d мс\n", a[NR]
        if (p95 < 5000) {
            printf "НФТ-11 (p95 < 5 c): выполняется на выборке %d\n", NR
        } else {
            printf "ВНИМАНИЕ: p95 >= 5000 мс — НФТ-11 не выполняется на выборке %d\n", NR
        }
    }'

echo
echo "Предупреждение: время аварии core-api/Redis, срабатывания лимита"
echo "30/час и паузы Retry-After в результат НЕ отфильтрованы (НФТ-11"
echo "исключает их из замера) — за чистотой протокола следит оператор."
