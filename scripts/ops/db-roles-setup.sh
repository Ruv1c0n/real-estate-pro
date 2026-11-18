#!/usr/bin/env bash
# db-roles-setup.sh — разовая выдача паролей сервисным ролям
# (миграция 0002_roles_per_service, PR #82; координация — «Координация» в его
# теле, шаги 1–2 выполняет этот скрипт как одноразовый сервис db-roles-setup).
#
# Семантика both-or-neither (ревью PR #82): пара *_DB_USER/*_DB_PASSWORD либо
# задана целиком, либо не задана вовсе. Неполная пара валит стек на старте
# с явной ошибкой, а не в рантайме через /health/ready.
#
# Пароль передаётся переменной psql (-v pwd=...), не литералом в команде:
# иначе он попадает в argv процесса (виден в ps / docker inspect) и в историю
# шелла. Значения секретов — только из .env; миграции пароли не содержат
# (правило 5 ТЗ 6.5), роли создаются LOGIN без пароля.

set -euo pipefail

errors=()

check_pair() {
  local label="$1" user="$2" pass="$3"
  if [ -n "$user" ] && [ -z "$pass" ]; then
    errors+=("${label}_DB_USER=${user} задан, но ${label}_DB_PASSWORD пуст — пара должна быть задана целиком (both-or-neither)")
  elif [ -z "$user" ] && [ -n "$pass" ]; then
    errors+=("${label}_DB_PASSWORD задан, но ${label}_DB_USER пуст — пара должна быть задана целиком (both-or-neither)")
  fi
}

check_pair "CORE_API" "${CORE_API_DB_USER:-}" "${CORE_API_DB_PASSWORD:-}"
check_pair "NOTIFIER" "${NOTIFIER_DB_USER:-}" "${NOTIFIER_DB_PASSWORD:-}"

if [ "${#errors[@]}" -gt 0 ]; then
  for m in "${errors[@]}"; do
    echo "ОШИБКА КОНФИГУРАЦИИ: $m" >&2
  done
  exit 1
fi

PGHOST="${PGHOST:-postgres}"
PGADMIN_USER="${POSTGRES_USER:-app}"
PGADMIN_DB="${POSTGRES_DB:-realestate}"
export PGPASSWORD="${POSTGRES_PASSWORD:-app}"

# Имя роли попадает в SQL — допускаем только идентификатор.
check_role_name() {
  if ! [[ "$1" =~ ^[a-z_][a-z0-9_]*$ ]]; then
    echo "ОШИБКА КОНФИГУРАЦИИ: имя роли '$1' не подходит под ^[a-z_][a-z0-9_]*\$" >&2
    exit 1
  fi
}

issue_password() {
  local role="$1" pass="$2"
  check_role_name "$role"
  # Роль создаёт миграция 0002; до её применения выдача невозможна —
  # падаем с явной ошибкой вместо тихого пропуска.
  if [ "$(psql -h "$PGHOST" -U "$PGADMIN_USER" -d "$PGADMIN_DB" -tAc \
      "SELECT count(*) FROM pg_roles WHERE rolname='${role}'")" != "1" ]; then
    echo "ОШИБКА: роль ${role} не найдена — сначала примените миграцию 0002_roles_per_service (alembic upgrade head)" >&2
    exit 1
  fi
  psql -h "$PGHOST" -U "$PGADMIN_USER" -d "$PGADMIN_DB" -v pwd="$pass" \
    -c "ALTER ROLE ${role} PASSWORD :'pwd'" >/dev/null
  echo "Пароль выдан роли ${role}"
}

if [ -n "${CORE_API_DB_USER:-}" ]; then
  issue_password "$CORE_API_DB_USER" "$CORE_API_DB_PASSWORD"
fi
if [ -n "${NOTIFIER_DB_USER:-}" ]; then
  issue_password "$NOTIFIER_DB_USER" "$NOTIFIER_DB_PASSWORD"
fi
if [ -z "${CORE_API_DB_USER:-}" ] && [ -z "${NOTIFIER_DB_USER:-}" ]; then
  echo "Сервисные роли не заданы — оба сервиса работают под общей ролью ${PGADMIN_USER} (режим по умолчанию)"
fi
