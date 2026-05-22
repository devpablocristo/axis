#!/usr/bin/env bash
# Smoke: Companion crea task → propose → Nexus governance persiste request y Companion guarda vínculo.
# Requiere: docker compose up (nexus + companion + companion-postgres), migraciones aplicadas,
# action_type companion.propose (migración Nexus 0009).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../lib/common.sh
source "$SCRIPT_DIR/../lib/common.sh"

echo "=== Smoke: Companion → Governance flow ==="

wait_for_http "$API_BASE/healthz"
wait_for_http "$COMPANION_BASE/readyz"
pass "Governance and Companion are up"

echo "Creating Companion task..."
CREATE_BODY=$(companion_post "/v1/tasks" "{\"title\":\"smoke-companion-$(date +%s)\",\"goal\":\"smoke e2e\",\"created_by\":\"smoke-script\"}")
TASK_ID=$(echo "$CREATE_BODY" | json_get 'id')
[ -n "$TASK_ID" ] && pass "Task created: $TASK_ID" || fail "No task id in response"

echo "Ensuring mock connector + execution plan (propose requires a plan)..."
CONNECTOR_ID=$(ensure_mock_connector)
[ -n "$CONNECTOR_ID" ] && pass "Mock connector ready: $CONNECTOR_ID" || fail "Could not ensure mock connector"
PLAN=$(companion_put "/v1/tasks/$TASK_ID/execution-plan" "{\"connector_id\":\"$CONNECTOR_ID\",\"operation\":\"mock.echo\",\"payload\":{\"message\":\"smoke review\"}}")
PLAN_OPERATION=$(echo "$PLAN" | json_get 'operation')
[ "$PLAN_OPERATION" = "mock.echo" ] && pass "Execution plan saved" || fail "Expected operation mock.echo, got $PLAN_OPERATION"

echo "Proposing to Governance..."
PROP=$(companion_post "/v1/tasks/$TASK_ID/propose" '{"note":"smoke propose"}')
REQ_ID=$(echo "$PROP" | json_get 'governance_submit.request_id')
[ -n "$REQ_ID" ] && pass "Propose returned governance_request_id: $REQ_ID" || fail "No governance_submit.request_id"

echo "Verifying request exists in Governance..."
RR=$(api_get "/v1/requests/$REQ_ID")
RR_ID=$(echo "$RR" | json_get 'id')
[ "$RR_ID" = "$REQ_ID" ] && pass "Governance GET request matches" || fail "Governance request id mismatch: $RR_ID vs $REQ_ID"

echo "Verifying Companion task detail links action to Governance..."
DETAIL=$(companion_get "/v1/tasks/$TASK_ID")
if echo "$DETAIL" | python3 -c "
import json, sys
d = json.load(sys.stdin)
rid = sys.argv[1]
acts = d.get('actions') or []
ok = any(a.get('governance_request_id') == rid for a in acts)
sys.exit(0 if ok else 1)
" "$REQ_ID"; then
  pass "Task detail contains action with governance_request_id"
else
  fail "Task detail missing governance_request_id on action"
fi

if echo "$DETAIL" | python3 -c "
import json, sys
d = json.load(sys.stdin)
rid = sys.argv[1]
sync = d.get('governance_sync') or {}
ok = sync.get('governance_request_id') == rid and bool(sync.get('last_checked_at'))
sys.exit(0 if ok else 1)
" "$REQ_ID"; then
  pass "Task detail exposes governance_sync snapshot"
else
  fail "Task detail missing governance_sync snapshot"
fi

RS=$(echo "$PROP" | json_get 'governance_submit.status')
case "$RS" in
  pending_approval)
    EXPECTED_ST="waiting_for_approval"
    ;;
  allowed|approved|executed)
    # Con execution plan vigente, una aprobación deja la task lista para
    # ejecutar (waiting_for_input), no done. Done lo cierra el ejecutor.
    EXPECTED_ST="waiting_for_input"
    ;;
  denied|rejected)
    EXPECTED_ST="failed"
    ;;
  *)
    fail "Unexpected governance_submit.status from propose: $RS (expected pending_approval, allowed, denied, …)"
    ;;
esac

TASK_ST=$(echo "$DETAIL" | json_get 'task.status')
if [ "$TASK_ST" = "$EXPECTED_ST" ]; then
  pass "Task status matches Governance outcome ($RS → $TASK_ST)"
else
  fail "Task status $TASK_ST != expected $EXPECTED_ST for governance_submit.status=$RS"
fi

echo "POST /v1/tasks/{id}/sync (manual / idempotent)..."
SYNC_BODY=$(companion_post "/v1/tasks/$TASK_ID/sync" '{}')
SYNC_ID=$(echo "$SYNC_BODY" | json_get 'id')
[ "$SYNC_ID" = "$TASK_ID" ] && pass "Sync returned task with same id" || fail "Sync id mismatch: $SYNC_ID vs $TASK_ID"

echo "Verifying tasks list exposes governance sync summary..."
LIST=$(companion_get "/v1/tasks?limit=20")
if echo "$LIST" | python3 -c "
import json, sys
data = json.load(sys.stdin).get('data') or []
task_id = sys.argv[1]
task = next((item for item in data if item.get('id') == task_id), None)
ok = (
    task is not None and
    bool(task.get('governance_status')) and
    bool(task.get('governance_last_checked_at'))
)
sys.exit(0 if ok else 1)
" "$TASK_ID"; then
  pass "Tasks list exposes governance_status and governance_last_checked_at"
else
  fail "Tasks list missing governance sync summary fields"
fi

echo ""
green "=== Companion ↔ Governance smoke passed ==="
