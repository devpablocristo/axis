#!/usr/bin/env bash
# Funciones compartidas para scripts de Nexus

set -euo pipefail

API_BASE="${API_BASE:-http://localhost:18084}"
API_KEY="${API_KEY:-nexus-admin-dev-key}"
API_KEY_ADMIN_A="${API_KEY_ADMIN_A:-${NEXUS_ADMIN_A_API_KEY:-nexus-admin-a-dev-key}}"
API_KEY_ADMIN_B="${API_KEY_ADMIN_B:-${NEXUS_ADMIN_B_API_KEY:-nexus-admin-b-dev-key}}"
API_KEY_OTHER_ORG="${API_KEY_OTHER_ORG:-${NEXUS_OTHER_ORG_ADMIN_API_KEY:-nexus-admin-other-dev-key}}"

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
  api_get_as "$API_KEY" "$1"
}

# POST con API key y body JSON
api_post() {
  api_post_as "$API_KEY" "$1" "$2"
}

api_get_as() {
  local key="$1"
  local path="$2"
  curl -sf -H "X-API-Key: $key" "$API_BASE$path"
}

api_post_as() {
  local key="$1"
  local path="$2"
  local body="${3-}"
  if [ -z "$body" ]; then
    body="{}"
  fi
  curl -sf -X POST -H "X-API-Key: $key" -H "Content-Type: application/json" -d "$body" "$API_BASE$path"
}

api_patch_as() {
  local key="$1"
  local path="$2"
  local body="$3"
  curl -sf -X PATCH -H "X-API-Key: $key" -H "Content-Type: application/json" -d "$body" "$API_BASE$path"
}

# DELETE con API key
api_delete() {
  api_delete_as "$API_KEY" "$1"
}

api_delete_as() {
  local key="$1"
  local path="$2"
  curl -sf -o /dev/null -w "%{http_code}" -X DELETE -H "X-API-Key: $key" "$API_BASE$path"
}

api_status_as() {
  local key="$1"
  local method="$2"
  local path="$3"
  local body="${4:-}"
  if [ -n "$body" ]; then
    curl -s -o /dev/null -w "%{http_code}" -X "$method" -H "X-API-Key: $key" -H "Content-Type: application/json" -d "$body" "$API_BASE$path"
  else
    curl -s -o /dev/null -w "%{http_code}" -X "$method" -H "X-API-Key: $key" "$API_BASE$path"
  fi
}

json_string() {
  python3 -c 'import json,sys; print(json.dumps(sys.argv[1]))' "$1"
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

pass() { green "PASS: $1"; }
fail() { red "FAIL: $1" >&2; exit 1; }
