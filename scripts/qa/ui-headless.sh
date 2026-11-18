#!/usr/bin/env bash
# ui-headless.sh — каркас проверки НФТ-15 (ТЗ 5.5): страницы списка/карточки
# открываются в реальном браузере, ошибки API и пустые результаты отображаются
# понятно (без сырого Jinja, трейсбеков и «Internal Server Error»).
#
# Каркас: рендерит каждую страницу в headless Edge (как в trail-viz) и
# проверяет DOM на признаки проблем. Полноценный прогон — когда появятся
# страницы collector'а (Дима); сейчас можно гнать по любым URL.
#
# Вход: URL'ы аргументами (default — страницы collector на стенде).
# EDGE — путь к msedge, определяем автоматически (Windows/Git Bash).

set -uo pipefail

EDGE="${EDGE:-}"
if [ -z "$EDGE" ]; then
    for p in \
        "/c/Program Files (x86)/Microsoft/Edge/Application/msedge.exe" \
        "/c/Program Files/Microsoft/Edge/Application/msedge.exe"; do
        [ -f "$p" ] && EDGE="$p" && break
    done
fi
[ -z "$EDGE" ] && { echo "msedge не найден — задай EDGE=/путь/msedge.exe" >&2; exit 2; }

fails=0
check() {
    url="$1"
    dom=$("$EDGE" --headless=new --disable-gpu --dump-dom --virtual-time-budget=3000 "$url" 2>/dev/null)
    if [ -z "$dom" ]; then
        echo "FAIL: $url — пустой DOM (страница не отрендерилась)"; fails=$((fails+1)); return
    fi
    problems=""
    case "$dom" in *'{{'*)  problems="$problems сырой-Jinja";; esac
    case "$dom" in *'Traceback'*) problems="$problems Traceback";; esac
    case "$dom" in *'Internal Server Error'*) problems="$problems 500";; esac
    case "$dom" in *'{error'*) problems="$problems сырой-JSON-ошибки";; esac
    if [ -n "$problems" ]; then
        echo "FAIL: $url —$problems"; fails=$((fails+1))
    else
        echo "OK:   $url"
    fi
}

if [ $# -gt 0 ]; then
    for u in "$@"; do check "$u"; done
else
    echo "URL'ы не заданы — каркас ждёт страницы collector'а (http://localhost:8001/...)."
    echo "Пример будущего прогона:"
    echo "  bash scripts/qa/ui-headless.sh http://localhost:8001/ http://localhost:8001/item/1 http://localhost:8001/?city=UnknownCity"
    echo "Проверено сейчас: ничего (страниц ещё нет — это осознанный каркас)."
fi
echo "Провалов: $fails"
exit $(( fails > 0 ))
