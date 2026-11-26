#!/usr/bin/env bash
# Расписание бэкапов внутри compose (карточка 06 плана prod-10of10,
# ТЗ 6.1: резервное копирование и проверка восстановления).
#
# Контейнер db-backup на образе postgres:16 ходит в сервис postgres по
# compose-сети напрямую (без docker-сокета и без host-docker):
#   - ежедневно в BACKUP_AT (по умолчанию 03:00, TZ=Europe/Minsk по ТЗ 3.2)
#     дамп -Fc + манифест (tbl|rows), как в scripts/db/backup.sh;
#   - маркер $BACKUP_DIR/.last-success (mtime = время последнего успешного
#     дампа) — основа алерта «бэкап старше 26 часов»; рядом с маркерами
#     textfile-совместимые backup_last_success_timestamp_seconds /
#     backup_last_verify_timestamp_seconds (строка «имя <unix>») — задел
#     под node-exporter textfile-collector (карточка 03);
#   - retention: 7 последних дневных + по воскресеньям копия в
#     realestate-weekly-*.dump, хранится 4;
#   - в BACKUP_VERIFY_DAY — проверка восстановления последнего дампа:
#     временный кластер внутри ЭТОГО же контейнера (initdb + pg_restore),
#     сверка числа строк каждой таблицы с манифестом, маркер .last-verify.
#
# Параметры окружения:
#   PG_USER/PG_PASSWORD/PG_DB (обязательны), PG_HOST (postgres), PG_PORT (5432),
#   BACKUP_DIR (/backups), BACKUP_AT (03:00), BACKUP_VERIFY_DAY (7 = Вс),
#   BACKUP_ON_START (1 — дамп сразу на старте, удобно для приёмки),
#   RETENTION_DAILY (7), RETENTION_WEEKLY (4) — прокидываются из .env
#   через compose (docker-compose.yml, сервис db-backup)
#
# Сбой дампа не убивает контейнер: цикл продолжается, маркер не обновляется
# — «старый маркер» и есть сигнал наблюдаемости джобы.

set -uo pipefail

# Дампы/манифесты могут содержать пользовательские данные — создаём их
# 0600 (umask 077), а не 0644; читает их только владелец контейнера.
umask 077

PG_USER="${PG_USER:?PG_USER не задан}"
PG_PASSWORD="${PG_PASSWORD:?PG_PASSWORD не задан}"
PG_DB="${PG_DB:?PG_DB не задан}"
PG_HOST="${PG_HOST:-postgres}"
PG_PORT="${PG_PORT:-5432}"
BACKUP_DIR="${BACKUP_DIR:-/backups}"
BACKUP_AT="${BACKUP_AT:-03:00}"
BACKUP_VERIFY_DAY="${BACKUP_VERIFY_DAY:-7}" # 1=Пн .. 7=Вс (date +%u)
BACKUP_ON_START="${BACKUP_ON_START:-1}"
RETENTION_DAILY="${RETENTION_DAILY:-7}"
RETENTION_WEEKLY="${RETENTION_WEEKLY:-4}"

log() { echo "[$(date '+%Y-%m-%dT%H:%M:%S%z')] $*"; }

# Fail-fast: BACKUP_AT должен быть HH:MM (24ч). Кривое значение ломает
# расчёт next_run (date -d ошибается → пустой/нулевой sleep → горячий
# цикл дампов по кругу).
if ! [[ "$BACKUP_AT" =~ ^([01]?[0-9]|2[0-3]):[0-5][0-9]$ ]]; then
    log "ОШИБКА: BACKUP_AT='$BACKUP_AT' — ожидается HH:MM (24ч), например 03:00"
    exit 1
fi

# wait_for_pg — стартовый дозор: postgres ещё поднимается (depends_on
# service_healthy снимает большую часть гонки, но re-подключение — НФТ-8).
wait_for_pg() {
    for _ in $(seq 1 60); do
        if PGPASSWORD="$PG_PASSWORD" pg_isready -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d "$PG_DB" >/dev/null 2>&1; then
            return 0
        fi
        sleep 5
    done
    log "postgres недоступен 5 минут — контейнер продолжит ждать по расписанию"
    return 1
}

dump() {
    local ts f manifest
    ts=$(date +%Y%m%d-%H%M%S)
    f="$BACKUP_DIR/realestate-$ts.dump"
    manifest="${f%.dump}.manifest.tsv"

    PGPASSWORD="$PG_PASSWORD" pg_dump -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" \
        -d "$PG_DB" -Fc -f "$f" || { log "ОШИБКА дампа"; return 1; }

    # Манифест: двухшаговый count(*) по каждой обычной таблице — тот же
    # формат, что у scripts/db/backup.sh (совместим с restore-verify.sh).
    # ВНИМАНИЕ: формат манифеста обязан совпадать в обоих скриптах:
    # scripts/db/backup.sh ↔ scripts/db/backup-loop.sh — правь синхронно.
    local counts_q
    counts_q=$(PGPASSWORD="$PG_PASSWORD" psql -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d "$PG_DB" -At -c "
SELECT COALESCE(string_agg(
    format('SELECT %L AS tbl, count(*) AS n FROM %I.%I', s || '.' || t, s, t),
    E'\nUNION ALL\n' ORDER BY s, t),
    'SELECT NULL AS tbl, NULL AS n WHERE false')
FROM (
    SELECT nspname AS s, relname AS t
    FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE c.relkind = 'r'
      AND n.nspname NOT IN ('pg_catalog', 'information_schema')
) t;") || { log "ОШИБКА построения манифеста"; rm -f "$f"; return 1; }

    PGPASSWORD="$PG_PASSWORD" psql -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d "$PG_DB" \
        -At -F '|' -c "$counts_q" > "$manifest" || { log "ОШИБКА манифеста"; rm -f "$f"; return 1; }

    touch "$BACKUP_DIR/.last-success"
    echo "backup_last_success_timestamp_seconds $(date +%s)" \
        > "$BACKUP_DIR/backup_last_success_timestamp_seconds"
    log "бэкап готов: $f ($(wc -c < "$f") байт), таблиц: $(wc -l < "$manifest")"
}

# verify_last_dump — проверка восстановления последнего дампа без docker:
# временный кластер поднимается в этом же контейнере (бинарники образа
# postgres:16), pg_restore, сверка строк с манифестом. Self-contained
# замена scripts/db/restore-verify.sh для расписания.
# Тело — под-оболочка `()`, не `{}`: EXIT-трап должен сработать на выходе
# из функции. В `{}`-варианте трап вешается на главный процесс, у которого
# вечный `while true` — кластер оставался бы висеть на :55432, и все
# следующие verify падали бы по занятому порту.
verify_last_dump() (
    local f manifest tmpdir pgpid="" mismatches=0 got
    # shellcheck disable=SC2012  # каталог наш, имена дампов контролируем
    f=$(ls -1t "$BACKUP_DIR"/realestate-[0-9]*.dump 2>/dev/null | head -1 || true)
    if [ -z "$f" ]; then
        log "verify: дампов нет — пропуск"
        return 0
    fi
    manifest="${f%.dump}.manifest.tsv"
    if [ ! -f "$manifest" ]; then
        log "verify: манифест не найден для $f — FAIL"
        return 1
    fi

    # Scratch в /tmp (1777), не в BACKUP_DIR: тот после umask 077 закрыт
    # 0700 и gosu postgres туда не доходит, а кластер пишет под postgres.
    tmpdir=$(mktemp -d)
    chmod 777 "$tmpdir" # postgres'у нужны запись/траверс; каталог одноразовый
    # Функция вызывается непрямым путём: trap cleanup EXIT ниже.
    # shellcheck disable=SC2329
    cleanup() {
        [ -n "$pgpid" ] && kill "$pgpid" 2>/dev/null
        rm -rf "$tmpdir"
    }
    trap cleanup EXIT

    log_pglog() {
        log "verify: лог временного кластера ($tmpdir/pg.log):"
        cat "$tmpdir/pg.log"
    }

    # initdb/postgres под root не работают («cannot be run as root») —
    # временный кластер живёт под postgres через gosu (есть в образе).
    gosu postgres initdb -D "$tmpdir/data" -U "$PG_USER" -A trust >/dev/null
    gosu postgres postgres -D "$tmpdir/data" -p 55432 -k "$tmpdir" > "$tmpdir/pg.log" 2>&1 &
    pgpid=$!
    for _ in $(seq 1 30); do
        pg_isready -q -h 127.0.0.1 -p 55432 && break
        sleep 1
    done
    # initdb создаёт пустой кластер (только postgres/template0/1): целевой
    # БД нет — создаём под тем же PG_USER, иначе pg_restore в неё падает.
    createdb -h 127.0.0.1 -p 55432 -U "$PG_USER" "$PG_DB" \
        || { log "verify: createdb FAIL"; log_pglog; return 1; }

    # --exit-on-error --no-owner --no-privileges — как в
    # scripts/db/restore-verify.sh: битый дамп FAIL сразу, а владельцы/гранты
    # боевого кластера (роли core_api/notifier) во временный не переносятся.
    pg_restore --exit-on-error --no-owner --no-privileges \
        -h 127.0.0.1 -p 55432 -U "$PG_USER" -d "$PG_DB" "$f" \
        || { log "verify: pg_restore FAIL"; log_pglog; return 1; }

    while IFS='|' read -r tbl n; do
        [ -n "$tbl" ] || continue
        got=$(psql -h 127.0.0.1 -p 55432 -U "$PG_USER" -d "$PG_DB" -At -c "SELECT count(*) FROM $tbl")
        if [ "$got" != "$n" ]; then
            log "verify: $tbl — в дампе $n, восстановлено $got"
            mismatches=$((mismatches + 1))
        fi
    done < "$manifest"

    if [ "$mismatches" -ne 0 ]; then
        log "verify: НЕ ПРОЙДЕНО ($mismatches расхождений)"
        log_pglog
        return 1
    fi
    touch "$BACKUP_DIR/.last-verify"
    echo "backup_last_verify_timestamp_seconds $(date +%s)" \
        > "$BACKUP_DIR/backup_last_verify_timestamp_seconds"
    log "verify: пройдено ($f)"
)

# prune — retention: хранятся RETENTION_DAILY последних дневных дампов
# (-mtime "+$((RETENTION_DAILY - 1))" — без -1 хранится на один дамп больше:
# off-by-one даёт 8 дампов при RETENTION_DAILY=7), weekly-копии не трогаются
# — их имя realestate-weekly-*, weekly хранится RETENTION_WEEKLY штук.
# Манифесты удаляются вместе с дампами.
prune() {
    find "$BACKUP_DIR" -maxdepth 1 -name 'realestate-[0-9]*.dump' -mtime "+$((RETENTION_DAILY - 1))" -print0 |
        while IFS= read -r -d '' f; do
            rm -f -- "$f" "${f%.dump}.manifest.tsv"
            log "retention: удалён $f"
        done
    # Пустой glob при set -o pipefail даёт non-zero и уронил бы prune →
    # свежий дамп помечался бы провальным; гасим и проверяем на пустоту.
    # shellcheck disable=SC2012  # каталог наш, имена дампов контролируем
    weeklies=$(ls -1t "$BACKUP_DIR"/realestate-weekly-*.dump 2>/dev/null || true)
    if [ -n "$weeklies" ]; then
        printf '%s\n' "$weeklies" |
            tail -n +$((RETENTION_WEEKLY + 1)) |
            while IFS= read -r f; do
                rm -f -- "$f" "${f%.dump}.manifest.tsv"
            done
    fi
}

# weekly_copy — по воскресеньям копия свежего дневного дампа под именем
# weekly-: живёт вне дневного retention (см. prune).
weekly_copy() {
    local newest f
    # shellcheck disable=SC2012  # каталог наш, имена дампов контролируем
    newest=$(ls -1t "$BACKUP_DIR"/realestate-[0-9]*.dump 2>/dev/null | head -1 || true)
    [ -n "$newest" ] || return 0
    f="$BACKUP_DIR/realestate-weekly-$(date +%Y%m%d).dump"
    cp -- "$newest" "$f"
    cp -- "${newest%.dump}.manifest.tsv" "${f%.dump}.manifest.tsv" 2>/dev/null || true
    log "weekly-копия: $f"
}

# next_run — секунды до ближайшего BACKUP_AT (GNU date образа postgres:16).
next_run() {
    local now target
    now=$(date +%s)
    target=$(date -d "today $BACKUP_AT" +%s 2>/dev/null || echo 0)
    if [ "$target" -le "$now" ]; then
        target=$(date -d "tomorrow $BACKUP_AT" +%s)
    fi
    echo $((target - now))
}

mkdir -p "$BACKUP_DIR"

if [ "$BACKUP_ON_START" = "1" ]; then
    # Стартовый дамп: свежий volume → мгновенно есть .last-success; также
    # это приёмка карточки 06 на живом стенде без ожидания ночи.
    if wait_for_pg; then
        if dump && prune; then
            : # стартовый дамп и prune прошли
        else
            log "стартовый бэкап не удался — продолжаем по расписанию"
        fi
    fi
fi

log "расписание: дампы в $BACKUP_AT (TZ=${TZ:-UTC}), verify по дню $BACKUP_VERIFY_DAY, retention $RETENTION_DAILY daily + $RETENTION_WEEKLY weekly"
while true; do
    s=$(next_run)
    # Sleep обязан быть положительным числом: пустой/нулевой/отрицательный
    # (кривые часы, кривой BACKUP_AT мимо fail-fast'а) → горячий цикл дампов.
    if [ -z "$s" ] || ! [[ "$s" =~ ^[0-9]+$ ]] || [ "$s" -le 0 ]; then
        log "WARN: next_run вернул '$s' — спим 60 секунд и пересчитываем"
        s=60
    fi
    sleep "$s"
    log "плановый дамп"
    if dump && prune; then
        if [ "$(date +%u)" = "$BACKUP_VERIFY_DAY" ]; then
            weekly_copy
            verify_last_dump || log "verify провален — маркер .last-verify не обновлён"
        fi
    else
        log "дамп не удался — повтор на следующем BACKUP_AT"
    fi
done
