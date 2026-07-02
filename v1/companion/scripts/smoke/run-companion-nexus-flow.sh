#!/usr/bin/env bash
# Smoke: Companion crea task → propose → Nexus persiste request y Companion guarda vínculo.
# Requiere: docker compose up (nexus + companion + companion-postgres), migraciones aplicadas,
# action_type companion.propose (migración Nexus 0009).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../lib/common.sh
source "$SCRIPT_DIR/../lib/common.sh"

echo "=== Smoke: Companion → Nexus flow ==="

wait_for_http "$API_BASE/readyz"
wait_for_http "$COMPANION_BASE/readyz"
pass "Nexus and Companion are up"

echo "Creating Companion task..."
CREATE_BODY=$(companion_post "/v1/tasks" "{\"title\":\"smoke-companion-$(date +%s)\",\"goal\":\"smoke e2e\",\"created_by\":\"smoke-script\"}")
TASK_ID=$(echo "$CREATE_BODY" | json_get 'id')
[ -n "$TASK_ID" ] && pass "Task created: $TASK_ID" || fail "No task id in response"

echo "Proposing to Nexus..."
PROP=$(companion_post "/v1/tasks/$TASK_ID/propose" '{"note":"smoke propose"}')
REQ_ID=$(echo "$PROP" | json_get 'nexus_submit.request_id')
[ -n "$REQ_ID" ] && pass "Propose returned nexus_request_id: $REQ_ID" || fail "No nexus_submit.request_id"

echo "Verifying request exists in Nexus..."
RR=$(api_get "/v1/requests/$REQ_ID")
RR_ID=$(echo "$RR" | json_get 'id')
[ "$RR_ID" = "$REQ_ID" ] && pass "Nexus GET request matches" || fail "Nexus request id mismatch: $RR_ID vs $REQ_ID"

echo "Verifying Companion task detail links action to Nexus..."
DETAIL=$(companion_get "/v1/tasks/$TASK_ID")
if echo "$DETAIL" | python3 -c "
import json, sys
d = json.load(sys.stdin)
rid = sys.argv[1]
acts = d.get('actions') or []
ok = any(a.get('nexus_request_id') == rid for a in acts)
sys.exit(0 if ok else 1)
" "$REQ_ID"; then
  pass "Task detail contains action with nexus_request_id"
else
  fail "Task detail missing nexus_request_id on action"
fi

if echo "$DETAIL" | python3 -c "
import json, sys
d = json.load(sys.stdin)
rid = sys.argv[1]
sync = d.get('nexus_sync') or {}
ok = sync.get('nexus_request_id') == rid and bool(sync.get('last_checked_at'))
sys.exit(0 if ok else 1)
" "$REQ_ID"; then
  pass "Task detail exposes nexus_sync snapshot"
else
  fail "Task detail missing nexus_sync snapshot"
fi

RS=$(echo "$PROP" | json_get 'nexus_submit.status')
case "$RS" in
  pending_approval)
    EXPECTED_ST="waiting_for_approval"
    ;;
  allowed|approved|executed)
    EXPECTED_ST="done"
    ;;
  denied|rejected)
    EXPECTED_ST="failed"
    ;;
  *)
    fail "Unexpected nexus_submit.status from propose: $RS (expected pending_approval, allowed, denied, …)"
    ;;
esac

TASK_ST=$(echo "$DETAIL" | json_get 'task.status')
if [ "$TASK_ST" = "$EXPECTED_ST" ]; then
  pass "Task status matches Nexus outcome ($RS → $TASK_ST)"
else
  fail "Task status $TASK_ST != expected $EXPECTED_ST for nexus_submit.status=$RS"
fi

echo "POST /v1/tasks/{id}/sync (manual / idempotent)..."
SYNC_BODY=$(companion_post "/v1/tasks/$TASK_ID/sync" '{}')
SYNC_ID=$(echo "$SYNC_BODY" | json_get 'id')
[ "$SYNC_ID" = "$TASK_ID" ] && pass "Sync returned task with same id" || fail "Sync id mismatch: $SYNC_ID vs $TASK_ID"

echo "Verifying tasks list exposes nexus sync summary..."
LIST=$(companion_get "/v1/tasks?limit=20")
if echo "$LIST" | python3 -c "
import json, sys
data = json.load(sys.stdin).get('data') or []
task_id = sys.argv[1]
task = next((item for item in data if item.get('id') == task_id), None)
ok = (
    task is not None and
    bool(task.get('nexus_status')) and
    bool(task.get('nexus_last_checked_at'))
)
sys.exit(0 if ok else 1)
" "$TASK_ID"; then
  pass "Tasks list exposes nexus_status and nexus_last_checked_at"
else
  fail "Tasks list missing nexus sync summary fields"
fi

echo ""
green "=== Companion ↔ Nexus smoke passed ==="
