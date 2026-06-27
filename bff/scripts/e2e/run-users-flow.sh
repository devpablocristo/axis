#!/usr/bin/env bash
# E2E: users module lifecycle against a real BFF in dev-mode. Exercises
# create → list → edit role → archive → restore → trash → purge for a
# tenant-scoped user (the flow that used to 500).
#
# Uses the running axis-control-postgres (sandbox blocks host access to ad-hoc
# `docker run -p` ports, but the compose pg is reachable). All data it creates is
# namespaced (e2e-*) and removed by the teardown trap — zero residue.
set -euo pipefail

BFF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PG_CONTAINER="${E2E_PG_CONTAINER:-axis-axis-control-postgres-1}"
PG_HOST_PORT="${E2E_PG_HOST_PORT:-15436}"
BFF_PORT="${E2E_BFF_PORT:-18090}"
BFF_URL="http://localhost:${BFF_PORT}"
DEV_USER="e2e-admin"
DEV_SCOPES="axis:cross_org axis:users:read axis:users:write axis:users:admin axis:iam:purge"
ORG_ID="e2e-co"
TENANT_ID="e2e-tn"
EMAIL="e2e-user@e2e-co.test"
WORKDIR="$(mktemp -d)"
BFF_PID=""

# Use the host psql client over the published port: `docker exec` stdout is not
# reliably captured in this sandbox, which breaks reading query results.
psql_exec() { PGPASSWORD=postgres psql -h localhost -p "$PG_HOST_PORT" -U postgres -d axis_control "$@"; }

cleanup() {
  [ -n "$BFF_PID" ] && kill -9 "$BFF_PID" 2>/dev/null || true
  # Remove every artifact this run created (cascades drop tenant + memberships).
  psql_exec -v ON_ERROR_STOP=0 -c \
    "DELETE FROM axis_users WHERE email='$EMAIL';
     DELETE FROM axis_orgs WHERE id='$ORG_ID';" >/dev/null 2>&1 || true
  rm -rf "$WORKDIR" 2>/dev/null || true
}
trap cleanup EXIT

fail() { echo "FAIL: $*"; [ -f "$WORKDIR/bff.log" ] && { echo "--- bff.log (tail) ---"; tail -20 "$WORKDIR/bff.log"; }; exit 1; }
pass() { echo "  ok: $*"; }

req() { # METHOD PATH [BODY] → sets $CODE and $BODY
  local method="$1" path="$2" body="${3:-}"
  local args=(-sS -X "$method" "$BFF_URL$path" -H "X-Dev-User-ID: $DEV_USER" -H "X-Dev-Scopes: $DEV_SCOPES" -H "X-Tenant-ID: $TENANT_ID" -w $'\n%{http_code}' -m 20)
  if [ -n "$body" ]; then args+=(-H "Content-Type: application/json" -d "$body"); fi
  local out; out="$(curl "${args[@]}")"
  CODE="$(printf '%s' "$out" | tail -n1)"
  BODY="$(printf '%s' "$out" | sed '$d')"
}

jget() { printf '%s' "$BODY" | python3 -c "import sys,json;d=json.load(sys.stdin);print(eval(\"d$1\"))"; }
list_has() { # STATUS_PATH EMAIL → yes/no
  req GET "/api/iam/users${1}?org_id=${ORG_ID}"
  [ "$CODE" = "200" ] || fail "list ($1) want 200 got $CODE body=$BODY"
  printf '%s' "$BODY" | python3 -c "import sys,json;d=json.load(sys.stdin);print('yes' if any(u.get('email')=='$2' for u in d.get('items',[])) else 'no')"
}

echo "== 0) preconditions + seed (idempotent) =="
command -v psql >/dev/null 2>&1 || fail "psql client not found on host (needed to seed/inspect)"
psql_exec -tAc 'SELECT 1' >/dev/null 2>&1 || fail "axis-control-postgres not reachable on :$PG_HOST_PORT — start it: docker compose up -d axis-control-postgres"
# Clean residue, then seed org + tenant idempotently (upsert) so the run is
# robust regardless of leftovers. Tables already exist in axis-control.
psql_exec -c "DELETE FROM axis_users WHERE email='$EMAIL'; DELETE FROM axis_orgs WHERE id='$ORG_ID';" >/dev/null 2>&1 || true
psql_exec -v ON_ERROR_STOP=1 -c \
  "INSERT INTO axis_orgs (id, provider, name, slug, status) VALUES ('$ORG_ID','dev','E2E Co','$ORG_ID','active') ON CONFLICT (id) DO UPDATE SET status='active';
   INSERT INTO axis_tenants (id, org_id, product_surface, name, status) VALUES ('$TENANT_ID','$ORG_ID','axis','E2E / axis','active') ON CONFLICT (id) DO UPDATE SET status='active';" >/dev/null
pass "postgres reachable; seeded org=$ORG_ID tenant=$TENANT_ID"

echo "== 1) build + boot bff (dev-mode) =="
( cd "$BFF_DIR" && go build -o "$WORKDIR/bff" . ) || fail "go build"
PORT="$BFF_PORT" \
AXIS_BFF_AUTH_MODE=dev \
AXIS_DEV_USER_ID="$DEV_USER" \
AXIS_CONTROL_DATABASE_URL="postgres://postgres:postgres@localhost:${PG_HOST_PORT}/axis_control?sslmode=disable" \
AXIS_INTERNAL_JWT_SECRET=e2e AXIS_INTERNAL_JWT_ISSUER=axis-bff \
COMPANION_BASE_URL="http://localhost:1/x" NEXUS_BASE_URL="http://localhost:1/x" \
"$WORKDIR/bff" >"$WORKDIR/bff.log" 2>&1 &
BFF_PID=$!
for i in $(seq 1 30); do [ "$(curl -sS -o /dev/null -w '%{http_code}' "$BFF_URL/healthz" -m 2 2>/dev/null)" = "200" ] && break; sleep 1; done
[ "$(curl -sS -o /dev/null -w '%{http_code}' "$BFF_URL/healthz" -m 2 2>/dev/null)" = "200" ] || { cat "$WORKDIR/bff.log"; fail "bff not healthy"; }
pass "bff up on :$BFF_PORT"

echo "== 2) create user in tenant =="
req POST "/api/iam/users" "{\"email\":\"$EMAIL\",\"role\":\"member\",\"org_id\":\"$ORG_ID\"}"
[ "$CODE" = "201" ] || fail "create want 201 got $CODE body=$BODY"
ROW_ID="$(jget "['item']['id']")"
[ "$ROW_ID" = "tenant__${TENANT_ID}__$(jget "['item']['user_id']")" ] || fail "row id not tenant-encoded: $ROW_ID"
pass "created $EMAIL ($ROW_ID)"

echo "== 4) list → present =="
[ "$(list_has "" "$EMAIL")" = "yes" ] || fail "user not in active list"
pass "listed in tenant"

echo "== 5) edit role → admin =="
req PUT "/api/iam/users/${ROW_ID}" "{\"role\":\"admin\",\"org_id\":\"$ORG_ID\"}"
[ "$CODE" = "200" ] || fail "edit want 200 got $CODE body=$BODY"
[ "$(jget "['item']['role']")" = "admin" ] || fail "role not updated: $BODY"
pass "role → admin"

echo "== 6) archive → leaves active, in archived =="
req POST "/api/iam/users/${ROW_ID}/archive"
[ "$CODE" = "200" ] || fail "archive want 200 got $CODE body=$BODY"
[ "$(list_has "" "$EMAIL")" = "no" ] || fail "still in active after archive"
[ "$(list_has "/archived" "$EMAIL")" = "yes" ] || fail "not in archived"
pass "archived"

echo "== 7) restore → back to active =="
req POST "/api/iam/users/${ROW_ID}/restore"
[ "$CODE" = "200" ] || fail "restore want 200 got $CODE body=$BODY"
[ "$(list_has "" "$EMAIL")" = "yes" ] || fail "not active after restore"
pass "restored"

echo "== 8) trash =="
req POST "/api/iam/users/${ROW_ID}/trash"
[ "$CODE" = "200" ] || fail "trash want 200 got $CODE body=$BODY"
[ "$(list_has "/trash" "$EMAIL")" = "yes" ] || fail "not in trash"
pass "trashed"

echo "== 9) purge → hard delete: gone from tenant AND from the IdP =="
req DELETE "/api/iam/users/${ROW_ID}/purge"
[ "$CODE" = "204" ] || fail "purge want 204 got $CODE body=$BODY"
for q in "" "/archived" "/trash"; do
  [ "$(list_has "$q" "$EMAIL")" = "no" ] || fail "still present in $q after purge"
done
# dev-mode deletes the local identity; against Clerk the adapter also issues
# DELETE /users/{id}. Identity must be gone (count=0).
COUNT="$(psql_exec -tAc "SELECT count(*) FROM axis_users WHERE email='$EMAIL';" | tr -d '[:space:]')"
[ "$COUNT" = "0" ] || fail "identity must be deleted on purge (count=$COUNT)"
pass "purged (user deleted, memberships cascaded)"

echo "ALL E2E USERS CHECKS PASSED"
