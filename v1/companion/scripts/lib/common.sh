#!/usr/bin/env bash
# Funciones compartidas para scripts smoke de Companion + Nexus

set -euo pipefail

# Cargar .env de Axis para alinear smoke con `docker compose` root.
_companion_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
_axis_root="$(cd "$_companion_root/.." && pwd)"
if [ "${COMPANION_SMOKE_LOAD_ENV:-1}" != "0" ] && [ -f "$_axis_root/.env" ]; then
  set -a
  # shellcheck disable=SC1091
  . "$_axis_root/.env"
  set +a
elif [ "${COMPANION_SMOKE_LOAD_ENV:-1}" != "0" ] && [ -f "$_companion_root/.env" ]; then
  set -a
  # shellcheck disable=SC1091
  . "$_companion_root/.env"
  set +a
fi
unset _companion_root
unset _axis_root

API_BASE="${API_BASE:-http://localhost:18084}"
# Llamadas desde el host al API de Nexus; debe ser la misma clave que NEXUS_API_KEY.
API_KEY="${NEXUS_API_KEY:-${API_KEY:-nexus-admin-dev-key}}"

# Companion (puerto host por defecto alineado con docker-compose)
COMPANION_BASE="${COMPANION_BASE:-http://localhost:18085}"
COMPANION_API_KEY="${COMPANION_ADMIN_API_KEY:-${COMPANION_API_KEY:-companion-admin-dev-key}}"

# Esperar a que un endpoint HTTP responda 200
wait_for_http() {
  local url="$1"
  local max_attempts="${2:-30}"
  local attempt=0
  while [ $attempt -lt $max_attempts ]; do
    if curl -sf "$url" > /dev/null 2>&1; then
      return 0
    fi
    attempt=$((attempt + 1))
    sleep 1
  done
  echo "ERROR: $url no respondió después de ${max_attempts}s" >&2
  return 1
}

# GET con API key
api_get() {
  local url="$API_BASE$1"
  local out code
  out=$(curl -sS -w "\n%{http_code}" -H "X-API-Key: $API_KEY" "$url") || return $?
  code=$(echo "$out" | tail -n1)
  out=$(echo "$out" | sed '$d')
  if ! [[ "$code" =~ ^[0-9]{3}$ ]] || [ "$code" -ge 400 ]; then
    echo "nexus GET $1 failed: HTTP ${code:-?} $out" >&2
    return 22
  fi
  printf '%s' "$out"
}

# POST con API key y body JSON
api_post() {
  local url="$API_BASE$1"
  local out code
  out=$(curl -sS -w "\n%{http_code}" -X POST -H "X-API-Key: $API_KEY" -H "Content-Type: application/json" -d "$2" "$url") || return $?
  code=$(echo "$out" | tail -n1)
  out=$(echo "$out" | sed '$d')
  if ! [[ "$code" =~ ^[0-9]{3}$ ]] || [ "$code" -ge 400 ]; then
    echo "nexus POST $1 failed: HTTP ${code:-?} $out" >&2
    return 22
  fi
  printf '%s' "$out"
}

# DELETE con API key
api_delete() {
  curl -sf -o /dev/null -w "%{http_code}" -X DELETE -H "X-API-Key: $API_KEY" "$API_BASE$1"
}

# Extraer campo JSON: json_get 'key' o json_get 'key.sub' o json_get 'len(key)'
json_get() {
  python3 -c "
import sys,json,re
d=json.load(sys.stdin)
path='$1'.strip('.')
m=re.match(r'len\((.+)\)',path)
if m:
    for k in m.group(1).split('.'):
        d=d[k]
    print(len(d))
else:
    for k in path.split('.'):
        d=d[k]
    print(d)
"
}

# Verificar HTTP status code
assert_status() {
  local actual="$1"
  local expected="$2"
  local context="${3:-}"
  if [ "$actual" != "$expected" ]; then
    echo "FAIL: expected HTTP $expected, got $actual ${context}" >&2
    return 1
  fi
}

# Color output
green() { echo -e "\033[32m$1\033[0m"; }
red() { echo -e "\033[31m$1\033[0m"; }
yellow() { echo -e "\033[33m$1\033[0m"; }

# GET Companion
companion_get() {
  local url="$COMPANION_BASE$1"
  local out code
  out=$(curl -sS -w "\n%{http_code}" -H "X-API-Key: $COMPANION_API_KEY" "$url") || return $?
  code=$(echo "$out" | tail -n1)
  out=$(echo "$out" | sed '$d')
  if ! [[ "$code" =~ ^[0-9]{3}$ ]] || [ "$code" -ge 400 ]; then
    echo "companion GET $1 failed: HTTP ${code:-?} $out" >&2
    return 22
  fi
  printf '%s' "$out"
}

# POST Companion JSON
companion_post() {
  local url="$COMPANION_BASE$1"
  local out code
  out=$(curl -sS -w "\n%{http_code}" -X POST -H "X-API-Key: $COMPANION_API_KEY" -H "Content-Type: application/json" -d "$2" "$url") || return $?
  code=$(echo "$out" | tail -n1)
  out=$(echo "$out" | sed '$d')
  if ! [[ "$code" =~ ^[0-9]{3}$ ]] || [ "$code" -ge 400 ]; then
    echo "companion POST $1 failed: HTTP ${code:-?} $out" >&2
    return 22
  fi
  printf '%s' "$out"
}

# PUT Companion JSON
companion_put() {
  local url="$COMPANION_BASE$1"
  local out code
  out=$(curl -sS -w "\n%{http_code}" -X PUT -H "X-API-Key: $COMPANION_API_KEY" -H "Content-Type: application/json" -d "$2" "$url") || return $?
  code=$(echo "$out" | tail -n1)
  out=$(echo "$out" | sed '$d')
  if ! [[ "$code" =~ ^[0-9]{3}$ ]] || [ "$code" -ge 400 ]; then
    echo "companion PUT $1 failed: HTTP ${code:-?} $out" >&2
    return 22
  fi
  printf '%s' "$out"
}

pass() { green "PASS: $1"; }
fail() { red "FAIL: $1" >&2; exit 1; }
