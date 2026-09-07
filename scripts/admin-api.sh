#!/usr/bin/env bash
# Запрос к админскому API Splitty через панель admin.zagirnur.dev.
#
# Панель выдаёт НЕ JWT, а подписанную куку admin_session (internal/auth/auth.go
# в репозитории админки): POST /api/login принимает только пароль, почта в
# проверке не участвует вовсе. Кука кладётся в jar и переиспользуется, пока
# жива, — повторный вход на каждый запрос не нужен.
#
# Пароль берётся из .env (ADMIN_PANEL_PASSWORD). Файл в .gitignore; в сам
# скрипт секреты не попадают.
#
#   ./scripts/admin-api.sh /projects/splitty/rooms?limit=200
#   ./scripts/admin-api.sh /projects/splitty/rooms/65a0000000000000000000ff
#
set -euo pipefail

BASE="${ADMIN_PANEL_URL:-https://admin.zagirnur.dev}"
ENV_FILE="${ENV_FILE:-$(dirname "$0")/../.env}"
JAR="${ADMIN_COOKIE_JAR:-${TMPDIR:-/tmp}/splitty-admin-session.jar}"

if [ $# -lt 1 ]; then
  echo "укажите путь, например /projects/splitty/rooms?limit=200" >&2
  exit 2
fi
PATH_ARG="$1"

if [ -z "${ADMIN_PANEL_PASSWORD:-}" ]; then
  if [ ! -r "$ENV_FILE" ]; then
    echo "нет $ENV_FILE и не задан ADMIN_PANEL_PASSWORD" >&2
    exit 2
  fi
  # Читаем только нужный ключ и только его: подключать весь .env через source
  # значило бы выполнить всё, что в нём лежит.
  ADMIN_PANEL_PASSWORD=$(grep -m1 '^ADMIN_PANEL_PASSWORD=' "$ENV_FILE" | cut -d= -f2-)
fi
if [ -z "${ADMIN_PANEL_PASSWORD:-}" ]; then
  echo "ADMIN_PANEL_PASSWORD пуст" >&2
  exit 2
fi

# Кука ещё жива? Пробуем запрос сразу — вход нужен только когда получили 401.
try_request() {
  curl -sS -m 60 -b "$JAR" -o /tmp/.admin-api-body.$$ -w '%{http_code}' "$BASE/api$PATH_ARG"
}

code=$(try_request || echo 000)
if [ "$code" = "401" ] || [ "$code" = "403" ] || [ ! -s "$JAR" ]; then
  login_code=$(printf '{"password":%s}' "$(printf '%s' "$ADMIN_PANEL_PASSWORD" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))')" \
    | curl -sS -m 60 -c "$JAR" -H 'Content-Type: application/json' \
        --data-binary @- -o /dev/null -w '%{http_code}' "$BASE/api/login")
  if [ "$login_code" != "200" ] && [ "$login_code" != "204" ]; then
    echo "вход не удался: HTTP $login_code" >&2
    exit 1
  fi
  chmod 600 "$JAR" 2>/dev/null || true
  code=$(try_request || echo 000)
fi

if [ "$code" != "200" ]; then
  echo "запрос не удался: HTTP $code" >&2
  cat /tmp/.admin-api-body.$$ >&2 || true
  rm -f /tmp/.admin-api-body.$$
  exit 1
fi
cat /tmp/.admin-api-body.$$
rm -f /tmp/.admin-api-body.$$
