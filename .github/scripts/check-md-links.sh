#!/usr/bin/env bash
# Проверка относительных ссылок и картинок в markdown-файлах репозитория.
# Выводит список битых ссылок; пустой вывод = всё в порядке.
# Внешние ссылки (http/https/mailto) и якоря (#section) не проверяются,
# чтобы CI не зависел от доступности чужих сайтов.
# Ограничение: ссылки внутри блоков кода (```) тоже проверяются — возможны
# ложные срабатывания на примерах в документации.
set -uo pipefail

while IFS= read -r f; do
  dir=$(dirname "$f")
  # [текст](цель) и ![alt](цель); grep -o оставляет только ](цель)
  grep -oE '\]\([^)]+\)' "$f" | sed -E 's/^\]\(//; s/\)$//' | while IFS= read -r tgt; do
    case "$tgt" in
      http://*|https://*|mailto:*|\#*) continue ;;
    esac
    path=${tgt%%#*}          # отбрасываем якорь после #
    [ -z "$path" ] && continue
    path=${path//%20/ }      # URL-декодирование пробелов
    if [ ! -e "$dir/$path" ]; then
      echo "$f: ссылка на несуществующий файл: $tgt"
    fi
  done
done < <(git ls-files '*.md')
