#!/usr/bin/env bash
# coreapi-p95.sh — каркас бенчмарка НФТ-10: p95 времени GET списка объявлений.
#
# ТЗ НФТ-10: для 500+ объявлений p95 времени GET списка — <300 мс с прогретым
# кэшем, <500 мс без кэша, при одном последовательном клиенте; 100 запросов
# после прогрева. В отчёте фиксируются машина, состав данных и фильтры;
# сетевое время до внешних площадок не учитывается (меряется только сам
# запрос к core-api).
#
# Использование (Git Bash):
#   bash scripts/bench/coreapi-p95.sh
#   COREAPI_BASE_URL=http://localhost:8000 N=100 WARMUP=10 \
#     FILTERS="deal_type=rent&city=Minsk&country=BY" \
#     bash scripts/bench/coreapi-p95.sh
#
# Переменные окружения:
#   COREAPI_BASE_URL — базовый URL core-api (default http://localhost:8000)
#   N                — число замеров после прогрева (default 100, по ТЗ)
#   WARMUP           — прогревочных запросов до замеров (default 10)
#   FILTERS          — query-строка фильтров; пусто = GET /api/v1/listings
#                      без query (default пусто)
#
# ВАЖНО: эндпоинт GET /api/v1/listings в core-api ещё не реализован
# (появится у Тимура) — при 404 скрипт внятно выходит. Это осознанный
# задел под issue #20, не баг.
#
# Замер «без кэша» (<500 мс) автоматом НЕ делается: нужна инвалидация
# кэша, которой скрипт не владеет. Как оператору измерить холодный прогон —
# см. scripts/bench/README.md, раздел «coreapi-p95.sh».

set -euo pipefail

BASE="${COREAPI_BASE_URL:-http://localhost:8000}"
N="${N:-100}"
WARMUP="${WARMUP:-10}"
FILTERS="${FILTERS:-}"

# sanity на параметры
if ! [[ "$N" =~ ^[0-9]+$ ]] || [[ "$N" -eq 0 ]]; then
  echo "ОШИБКА: N должно быть положительным целым (получено: '$N')." >&2
  exit 3
fi
if ! [[ "$WARMUP" =~ ^[0-9]+$ ]]; then
  echo "ОШИБКА: WARMUP должно быть целым (получено: '$WARMUP')." >&2
  exit 3
fi

LISTINGS_URL="${BASE}/api/v1/listings"
if [[ -n "$FILTERS" ]]; then
  LISTINGS_URL="${LISTINGS_URL}?${FILTERS}"
fi

# --- 1. доступность core-api по /health (liveness) ---
health_code="$(curl -o /dev/null -s -w '%{http_code}' --max-time 5 "${BASE}/health" || true)"
if [[ "$health_code" != "200" ]]; then
  echo "ОШИБКА: core-api не поднят (GET ${BASE}/health -> ${health_code:-нет ответа})." >&2
  echo "Поднимите стенд: docker compose up --build (нужен запущенный Docker Desktop)." >&2
  exit 1
fi

# --- 2. проверка самого эндпоинта (пока его нет — осознанный каркас) ---
probe_code="$(curl -o /dev/null -s -w '%{http_code}' --max-time 5 "$LISTINGS_URL" || true)"
if [[ "$probe_code" == "404" ]]; then
  echo "ОШИБКА: GET ${LISTINGS_URL} -> 404." >&2
  echo "Эндпоинт GET /api/v1/listings ещё не реализован — это каркас для issue #20." >&2
  echo "Скрипт станет рабочим, как только core-api отдаст список объявлений." >&2
  exit 2
fi
if [[ "$probe_code" != "200" ]]; then
  echo "ОШИБКА: GET ${LISTINGS_URL} -> ${probe_code:-нет ответа} (ожидали 200)." >&2
  exit 2
fi

# --- 3. прогрев кэша: WARMUP запросов без замера ---
echo "Прогрев кэша: ${WARMUP} запросов к ${LISTINGS_URL} ..."
for ((i = 1; i <= WARMUP; i++)); do
  curl -o /dev/null -s --max-time 30 "$LISTINGS_URL" || true
done

# --- 4. N последовательных замеров (один клиент, по ТЗ) ---
latencies=()
for ((i = 1; i <= N; i++)); do
  t="$(curl -o /dev/null -s -w '%{time_total}' --max-time 30 "$LISTINGS_URL" || true)"
  if [[ -z "$t" ]]; then
    echo "ПРЕДУПРЕЖДЕНИЕ: замер ${i} не удался (curl не вернул time_total) — пропущен." >&2
    continue
  fi
  # секунды -> миллисекунды, один знак после запятой
  latencies+=("$(awk -v s="$t" 'BEGIN { printf "%.1f", s * 1000 }')")
done
count="${#latencies[@]}"
if [[ "$count" -eq 0 ]]; then
  echo "ОШИБКА: ни один замер не удался — p95 посчитать нельзя." >&2
  exit 2
fi
if [[ "$count" -lt "$N" ]]; then
  echo "ПРЕДУПРЕЖДЕНИЕ: успешных замеров ${count} из ${N} — перцентили по ним." >&2
fi
if [[ "$N" -lt 100 ]]; then
  echo "ПРЕДУПРЕЖДЕНИЕ: по ТЗ НФТ-10 нужно 100 запросов после прогрева, задано N=${N}." >&2
fi

# --- 5. перцентили: сортировка, индексы по ceil(p*n), 1-based ---
stats="$(printf '%s\n' "${latencies[@]}" | sort -n | awk -v n="$count" '
  { v[NR] = $1 }
  END {
    p50 = v[int(0.50 * n + 0.999)]
    p95 = v[int(0.95 * n + 0.999)]
    mx  = v[n]
    printf "%.1f %.1f %.1f\n", p50, p95, mx
  }')"
p50="${stats%% *}"
rest="${stats#* }"
p95="${rest%% *}"
max="${rest##* }"

# --- 6. отчёт ---
echo ""
echo "=== НФТ-10: GET /api/v1/listings (с прогретым кэшем) ==="
echo "База:      ${BASE}"
echo "Фильтры:   ${FILTERS:-<нет>}"
echo "Прогрев:   ${WARMUP} запросов"
echo "Замеров:   ${count} (последовательный клиент)"
echo ""
echo "p50: ${p50} мс"
echo "p95: ${p95} мс"
echo "max: ${max} мс"
echo ""
if awk -v p="$p95" 'BEGIN { exit !(p + 0 < 300) }'; then
  echo "verdict: НФТ-10 (<300 мс с кэшем): выполняется"
else
  echo "verdict: НФТ-10 (<300 мс с кэшем): НЕ выполняется (p95=${p95} мс)"
fi
echo ""
echo "# Напоминание (ТЗ): в отчёте фиксируются машина, состав данных (500+ объявлений)"
echo "# и фильтры. Прогон без кэша (<500 мс) делается отдельно — см. README.md."
