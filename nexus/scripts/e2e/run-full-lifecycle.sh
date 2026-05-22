#!/usr/bin/env bash
# E2E: ciclo completo request → policy eval → approval → execution → replay → learning
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../lib/common.sh"

POLICY_ID=""
cleanup() {
  if [ -n "$POLICY_ID" ]; then
    api_delete "/v1/policies/$POLICY_ID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

wait_for_request_status() {
  local request_id="$1"
  local expected="$2"
  local attempts="${3:-10}"
  local delay="${4:-1}"
  local current=""
  local i=0
  while [ "$i" -lt "$attempts" ]; do
    current=$(api_get "/v1/requests/$request_id" | json_get 'status')
    if [ "$current" = "$expected" ]; then
      echo "$current"
      return 0
    fi
    i=$((i + 1))
    sleep "$delay"
  done
  echo "$current"
  return 1
}

approval_pending_snapshot() {
  local approval_id="$1"
  api_get "/v1/approvals/pending" | python3 -c "
import json, sys
approval_id = sys.argv[1]
data = json.load(sys.stdin).get('data') or []
approval = next((item for item in data if item.get('id') == approval_id), None)
if approval is None:
    raise SystemExit(1)
print(json.dumps(approval))
" "$approval_id"
}

approve_until_resolved() {
  local approval_id="$1"
  local max_approvers="${2:-5}"
  local idx=1
  while [ "$idx" -le "$max_approvers" ]; do
    api_post "/v1/approvals/$approval_id/approve" "{\"decided_by\":\"e2e-admin-$idx@co\",\"note\":\"E2E test approval $idx\"}" >/dev/null
    if ! approval_pending_snapshot "$approval_id" >/dev/null 2>&1; then
      return 0
    fi
    idx=$((idx + 1))
  done
  return 1
}

echo "=== E2E: full lifecycle ==="

wait_for_http "$API_BASE/healthz"

# Setup: crear policy
POLICY=$(api_post "/v1/policies" '{"name":"e2e-require-approval","expression":"request.action_type == '\''alert.escalate'\''","effect":"require_approval","priority":5,"enabled":true}')
POLICY_ID=$(echo "$POLICY" | json_get 'id')
pass "Setup: policy created"

# 1. Submit request que matchea policy
R=$(api_post "/v1/requests" '{"requester_type":"service","requester_id":"deploy-svc","action_type":"alert.escalate","target_system":"internal","target_resource":"api-gateway","reason":"memory leak detected","context":"RSS 4.2GB, threshold 4GB"}')
REQUEST_ID=$(echo "$R" | json_get 'request_id')
DECISION=$(echo "$R" | json_get 'decision')
APPROVAL_ID=$(echo "$R" | json_get 'approval.id')
[ "$DECISION" = "require_approval" ] && pass "1. Request requires approval" || fail "Expected require_approval"

# 2. Verificar que aparece en pending approvals
PENDING=$(api_get "/v1/approvals/pending")
FOUND=$(echo "$PENDING" | python3 -c "import sys,json; d=json.load(sys.stdin)['data']; print(any(a['id']=='$APPROVAL_ID' for a in d))")
[ "$FOUND" = "True" ] && pass "2. Found in pending approvals" || fail "Not in pending"

# 3. Verificar request status
REQ=$(api_get "/v1/requests/$REQUEST_ID")
STATUS=$(echo "$REQ" | json_get 'status')
[ "$STATUS" = "pending_approval" ] && pass "3. Request status: pending_approval" || fail "Expected pending_approval, got $STATUS"

# 4. Approve
if approve_until_resolved "$APPROVAL_ID"; then
  pass "4. Approved"
else
  SNAPSHOT=$(approval_pending_snapshot "$APPROVAL_ID" || true)
  fail "Approval did not resolve: ${SNAPSHOT:-still pending}"
fi

# 5. Verificar request cambió a approved
if STATUS=$(wait_for_request_status "$REQUEST_ID" "approved"); then
  pass "5. Request status: approved"
else
  fail "Expected approved, got $STATUS"
fi

# 6. Report result
api_post "/v1/requests/$REQUEST_ID/result" '{"success":true,"result":{"restart":"completed"},"duration_ms":2500}'
pass "6. Result reported"

# 7. Verificar request cambió a executed
if STATUS=$(wait_for_request_status "$REQUEST_ID" "executed"); then
  pass "7. Request status: executed"
else
  fail "Expected executed, got $STATUS"
fi

# 8. Replay completo
REPLAY=$(api_get "/v1/requests/$REQUEST_ID/replay")
FINAL=$(echo "$REPLAY" | json_get 'final_status')
EVENTS=$(echo "$REPLAY" | json_get 'len(timeline)')
[ "$FINAL" = "executed" ] && pass "8. Replay final_status: executed" || fail "Expected executed"
[ "$EVENTS" -ge 4 ] && pass "   Replay has $EVENTS events" || fail "Expected >= 4 events"

# 9. Dashboard refleja los datos
METRICS=$(api_get "/v1/metrics/summary")
TOTAL=$(echo "$METRICS" | json_get 'total_requests')
[ "$TOTAL" -ge 1 ] && pass "9. Dashboard: $TOTAL requests" || fail "Dashboard empty"

# 10. Learning analyze (no debería generar propuestas con tan pocas requests)
ANALYZE=$(api_post "/v1/learning/analyze" '{}')
CREATED=$(echo "$ANALYZE" | json_get 'proposals_created')
pass "10. Learning analyze: $CREATED proposals"

# 11. Idempotency
R_IDEM1=$(api_post "/v1/requests" '{"requester_type":"agent","requester_id":"idem-bot","action_type":"alert.escalate"}' | json_get 'request_id')
# Necesitamos header Idempotency-Key para la segunda
R_IDEM2=$(curl -sf -X POST -H "X-API-Key: $API_KEY" -H "Content-Type: application/json" -H "Idempotency-Key: e2e-idem-key" \
  -d '{"requester_type":"agent","requester_id":"idem-bot","action_type":"alert.escalate"}' "$API_BASE/v1/requests" | json_get 'request_id')
R_IDEM3=$(curl -sf -X POST -H "X-API-Key: $API_KEY" -H "Content-Type: application/json" -H "Idempotency-Key: e2e-idem-key" \
  -d '{"requester_type":"agent","requester_id":"idem-bot","action_type":"alert.escalate"}' "$API_BASE/v1/requests" | json_get 'request_id')
[ "$R_IDEM2" = "$R_IDEM3" ] && pass "11. Idempotency: same ID" || fail "Idempotency failed: $R_IDEM2 vs $R_IDEM3"

echo ""
green "=== E2E full lifecycle passed ==="
