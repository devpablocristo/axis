#!/usr/bin/env bash
# Smoke: Companion propose -> Nexus DENY policy -> task transitions to failed.
# Cubre el anti-happy-path del flow companion ↔ governance.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../lib/common.sh
source "$SCRIPT_DIR/../lib/common.sh"

POLICY_ID=""
cleanup() {
  if [ -n "$POLICY_ID" ]; then
    api_delete "/v1/policies/$POLICY_ID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

echo "=== Smoke: Companion → Governance DENIED flow ==="

wait_for_http "$API_BASE/healthz"
wait_for_http "$COMPANION_BASE/readyz"
pass "Governance and Companion are up"

echo "Creating DENY policy for companion.propose..."
POLICY=$(api_post "/v1/policies" "{\"name\":\"smoke-companion-deny-$(date +%s)\",\"expression\":\"request.action_type == 'companion.propose'\",\"effect\":\"deny\",\"priority\":1,\"enabled\":true}")
POLICY_ID=$(echo "$POLICY" | json_get 'id')
[ -n "$POLICY_ID" ] && pass "Deny policy created: $POLICY_ID" || fail "Policy id missing"

echo "Creating task + connector + plan..."
CREATE_BODY=$(companion_post "/v1/tasks" "{\"title\":\"smoke-denied-$(date +%s)\",\"goal\":\"governance should deny\",\"created_by\":\"smoke-script\"}")
TASK_ID=$(echo "$CREATE_BODY" | json_get 'id')
[ -n "$TASK_ID" ] && pass "Task created: $TASK_ID" || fail "No task id in response"

CONNECTOR_ID=$(ensure_mock_connector)
[ -n "$CONNECTOR_ID" ] && pass "Mock connector ready: $CONNECTOR_ID" || fail "Could not ensure mock connector"

companion_put "/v1/tasks/$TASK_ID/execution-plan" "{\"connector_id\":\"$CONNECTOR_ID\",\"operation\":\"mock.echo\",\"payload\":{\"message\":\"smoke deny\"}}" >/dev/null
pass "Execution plan saved"

echo "Proposing — expecting denied..."
PROP=$(companion_post "/v1/tasks/$TASK_ID/propose" '{"note":"smoke deny propose"}')
STATUS=$(echo "$PROP" | json_get 'governance_submit.status')
case "$STATUS" in
  denied|rejected)
    pass "Governance returned $STATUS as expected"
    ;;
  *)
    fail "Expected denied/rejected, got governance_submit.status=$STATUS"
    ;;
esac

echo "Verifying task transitioned to failed..."
DETAIL=$(companion_get "/v1/tasks/$TASK_ID")
TASK_ST=$(echo "$DETAIL" | json_get 'task.status')
[ "$TASK_ST" = "failed" ] && pass "Task FSM transitioned to failed" || fail "Expected task.status failed, got $TASK_ST"

echo "Verifying task detail still exposes governance request linkage..."
if echo "$DETAIL" | python3 -c "
import json, sys
d = json.load(sys.stdin)
sync = d.get('governance_sync') or {}
ok = bool(sync.get('governance_request_id')) and bool(sync.get('last_checked_at'))
sys.exit(0 if ok else 1)
"; then
  pass "Task detail still exposes governance_sync after deny"
else
  fail "Task detail missing governance_sync after deny"
fi

echo ""
green "=== Companion denied flow smoke passed ==="
