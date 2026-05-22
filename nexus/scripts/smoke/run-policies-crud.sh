#!/usr/bin/env bash
# Smoke test: CRUD completo de policies (7 operaciones)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../lib/common.sh"

echo "=== Smoke: policies CRUD ==="

wait_for_http "$API_BASE/healthz"

# 1. Create
P=$(api_post "/v1/policies" '{"name":"crud-test","expression":"true","effect":"allow","priority":50,"enabled":true}')
PID=$(echo "$P" | json_get 'id')
pass "Create: $PID"

# 2. Read
api_get "/v1/policies/$PID" > /dev/null
pass "Read"

# 3. List
LIST=$(api_get "/v1/policies")
COUNT=$(echo "$LIST" | json_get 'len(data)')
[ "$COUNT" -ge 1 ] && pass "List: $COUNT policies" || fail "List empty"

# 4. Update (PATCH)
curl -sf -X PATCH -H "X-API-Key: $API_KEY" -H "Content-Type: application/json" \
  -d '{"name":"crud-updated"}' "$API_BASE/v1/policies/$PID" > /dev/null
pass "Update"

# 5. Archive
STATUS=$(curl -sf -o /dev/null -w "%{http_code}" -X POST -H "X-API-Key: $API_KEY" "$API_BASE/v1/policies/$PID/archive")
assert_status "$STATUS" "204" "archive"
pass "Archive"

# 6. Restore
STATUS=$(curl -sf -o /dev/null -w "%{http_code}" -X POST -H "X-API-Key: $API_KEY" "$API_BASE/v1/policies/$PID/restore")
assert_status "$STATUS" "204" "restore"
pass "Restore"

# 7. Delete (hard)
STATUS=$(api_delete "/v1/policies/$PID")
assert_status "$STATUS" "204" "delete"
pass "Delete (hard)"

# Verify deleted
HTTP=$(curl -s -o /dev/null -w "%{http_code}" -H "X-API-Key: $API_KEY" "$API_BASE/v1/policies/$PID")
assert_status "$HTTP" "404" "verify deleted"
pass "Verified deleted (404)"

echo ""
green "=== Policies CRUD smoke passed ==="
