#!/usr/bin/env bash
# Smoke: Companion execution_plan -> Governance approval -> sync -> execute.
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

echo "=== Smoke: Companion execution flow ==="

wait_for_http "$API_BASE/healthz"
wait_for_http "$COMPANION_BASE/readyz"
pass "Governance and Companion are up"

echo "Creating require-approval policy for companion.propose..."
POLICY=$(api_post "/v1/policies" "{\"name\":\"smoke-companion-exec-$(date +%s)\",\"expression\":\"request.action_type == 'companion.propose'\",\"effect\":\"require_approval\",\"priority\":5,\"enabled\":true}")
POLICY_ID=$(echo "$POLICY" | json_get 'id')
[ -n "$POLICY_ID" ] && pass "Policy created: $POLICY_ID" || fail "Policy id missing"

echo "Creating Companion task..."
CREATE_BODY=$(companion_post "/v1/tasks" "{\"title\":\"smoke-companion-exec-$(date +%s)\",\"goal\":\"execute mock connector\",\"created_by\":\"smoke-script\"}")
TASK_ID=$(echo "$CREATE_BODY" | json_get 'id')
[ -n "$TASK_ID" ] && pass "Task created: $TASK_ID" || fail "No task id in response"

echo "Ensuring mock connector is registered and enabled..."
CONNECTOR_ID=$(ensure_mock_connector)
[ -n "$CONNECTOR_ID" ] && pass "Mock connector ready: $CONNECTOR_ID" || fail "Could not ensure mock connector"

echo "Saving execution plan..."
PLAN=$(companion_put "/v1/tasks/$TASK_ID/execution-plan" "{\"connector_id\":\"$CONNECTOR_ID\",\"operation\":\"mock.echo\",\"payload\":{\"message\":\"smoke execution\"}}")
PLAN_OPERATION=$(echo "$PLAN" | json_get 'operation')
[ "$PLAN_OPERATION" = "mock.echo" ] && pass "Execution plan saved" || fail "Expected operation mock.echo, got $PLAN_OPERATION"

echo "Proposing task to Governance..."
PROP=$(companion_post "/v1/tasks/$TASK_ID/propose" '{"note":"smoke execution propose"}')
REQ_ID=$(echo "$PROP" | json_get 'governance_submit.request_id')
REQ_STATUS=$(echo "$PROP" | json_get 'governance_submit.status')
[ -n "$REQ_ID" ] && pass "Propose returned governance_request_id: $REQ_ID" || fail "Missing governance_submit.request_id"

echo "Fetching Governance request..."
REQUEST=$(api_get "/v1/requests/$REQ_ID")
REQUEST_STATUS=$(echo "$REQUEST" | json_get 'status')
APPROVAL_ID=""
pass "Governance request status: $REQUEST_STATUS"

if [ "$REQUEST_STATUS" = "pending_approval" ]; then
  PENDING=$(api_get "/v1/approvals/pending")
  APPROVAL_ID=$(echo "$PENDING" | python3 -c "
import json, sys
data = json.load(sys.stdin).get('data') or []
request_id = sys.argv[1]
approval = next((item for item in data if item.get('request_id') == request_id), None)
if not approval:
    raise SystemExit(1)
print(approval['id'])
" "$REQ_ID")
  [ -n "$APPROVAL_ID" ] || fail "Expected approval_id for pending request"
  echo "Approving Governance request..."
  api_post "/v1/approvals/$APPROVAL_ID/approve" '{"decided_by":"smoke-admin","note":"smoke execution approval"}' >/dev/null
  pass "Approval completed"
else
  case "$REQUEST_STATUS" in
    allowed|approved|executed)
      pass "Request already resolved without manual approval"
      ;;
    *)
      fail "Unexpected request status after propose: $REQUEST_STATUS (submit status: $REQ_STATUS)"
      ;;
  esac
fi

echo "Syncing Companion task with Governance..."
SYNC=$(companion_post "/v1/tasks/$TASK_ID/sync" '{}')
SYNC_STATUS=$(echo "$SYNC" | json_get 'status')
[ "$SYNC_STATUS" = "waiting_for_input" ] && pass "Task is ready for execution" || fail "Expected waiting_for_input after sync, got $SYNC_STATUS"

echo "Executing task..."
EXEC=$(companion_post "/v1/tasks/$TASK_ID/execute" '{}')
EXEC_TASK_STATUS=$(echo "$EXEC" | json_get 'task.status')
EXEC_STATUS=$(echo "$EXEC" | json_get 'execution.status')
EXEC_OPERATION=$(echo "$EXEC" | json_get 'execution.operation')
[ "$EXEC_TASK_STATUS" = "done" ] && pass "Task execution finalized as done" || fail "Expected task.status done, got $EXEC_TASK_STATUS"
[ "$EXEC_STATUS" = "success" ] && pass "Execution status is success" || fail "Expected execution.status success, got $EXEC_STATUS"
[ "$EXEC_OPERATION" = "mock.echo" ] && pass "Execution used mock.echo" || fail "Expected execution.operation mock.echo, got $EXEC_OPERATION"

echo "Verifying task detail..."
DETAIL=$(companion_get "/v1/tasks/$TASK_ID")
if echo "$DETAIL" | python3 -c "
import json, sys
d = json.load(sys.stdin)
task = d.get('task') or {}
plan = d.get('execution_plan') or {}
artifacts = d.get('artifacts') or []
actions = d.get('actions') or []
sync = d.get('governance_sync') or {}
artifact_kinds = {item.get('kind') for item in artifacts}
action_types = {item.get('action_type') for item in actions}
ok = (
    task.get('status') == 'done' and
    plan.get('operation') == 'mock.echo' and
    (d.get('execution_state') or {}).get('verification_result', {}).get('status') == 'verified' and
    (d.get('execution_state') or {}).get('retry_count') == 0 and
    (d.get('execution_state') or {}).get('retryable') is False and
    sync.get('last_governance_status') in {'allowed', 'approved', 'executed'} and
    'connector_execution' in artifact_kinds and
    'execution_verification' in artifact_kinds and
    'execute_connector' in action_types
)
sys.exit(0 if ok else 1)
"; then
  pass "Task detail exposes execution plan, execution state, governance sync and artifacts"
else
  fail "Task detail missing execution evidence"
fi

echo "Verifying tasks list summary..."
LIST=$(companion_get "/v1/tasks?limit=20")
if echo "$LIST" | python3 -c "
import json, sys
data = json.load(sys.stdin).get('data') or []
task_id = sys.argv[1]
task = next((item for item in data if item.get('id') == task_id), None)
ok = (
    task is not None and
    task.get('status') == 'done' and
    task.get('governance_status') in {'allowed', 'approved', 'executed'} and
    bool(task.get('governance_last_checked_at'))
)
sys.exit(0 if ok else 1)
" "$TASK_ID"; then
  pass "Tasks list exposes execution summary"
else
  fail "Tasks list missing execution summary"
fi

echo ""
green "=== Companion execution smoke passed ==="
