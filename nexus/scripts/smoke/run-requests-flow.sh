#!/usr/bin/env bash
# Smoke test: flow completo de requests (create policy → submit → approve → result → replay)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../lib/common.sh"

echo "=== Smoke: requests flow ==="

wait_for_http "$API_BASE/healthz"

# 1. Crear policy
echo "Creating policy..."
POLICY=$(api_post "/v1/policies" '{"name":"smoke-policy","expression":"request.action_type == '\''alert.silence'\''","effect":"require_approval","priority":10,"enabled":true}')
POLICY_ID=$(echo "$POLICY" | json_get 'id')
pass "Policy created: $POLICY_ID"

# 2. Submit request (allow)
echo "Submitting request (allow)..."
R1=$(api_post "/v1/requests" '{"requester_type":"agent","requester_id":"smoke-bot","action_type":"alert.escalate","reason":"smoke test"}')
R1_DECISION=$(echo "$R1" | json_get 'decision')
[ "$R1_DECISION" = "allow" ] && pass "Request allowed" || fail "Expected allow, got $R1_DECISION"

# 3. Submit request (require_approval)
echo "Submitting request (require_approval)..."
R2=$(api_post "/v1/requests" '{"requester_type":"agent","requester_id":"smoke-bot","action_type":"alert.silence","target_system":"pagerduty","target_resource":"CPU-SMOKE","reason":"smoke test"}')
R2_ID=$(echo "$R2" | json_get 'request_id')
R2_DECISION=$(echo "$R2" | json_get 'decision')
APPROVAL_ID=$(echo "$R2" | json_get 'approval.id')
[ "$R2_DECISION" = "require_approval" ] && pass "Request requires approval" || fail "Expected require_approval, got $R2_DECISION"

# 4. List pending approvals
echo "Listing pending approvals..."
PENDING=$(api_get "/v1/approvals/pending")
PENDING_COUNT=$(echo "$PENDING" | json_get 'len(data)')
[ "$PENDING_COUNT" -ge 1 ] && pass "Pending approvals: $PENDING_COUNT" || fail "No pending approvals"

# 5. Approve
echo "Approving..."
api_post "/v1/approvals/$APPROVAL_ID/approve" '{"decided_by":"smoke-admin","note":"smoke approved"}'
pass "Approved"

# 6. Report result
echo "Reporting result..."
api_post "/v1/requests/$R2_ID/result" '{"success":true,"result":{"smoke":"ok"},"duration_ms":100}'
pass "Result reported"

# 7. Replay
echo "Fetching replay..."
REPLAY=$(api_get "/v1/requests/$R2_ID/replay")
EVENTS=$(echo "$REPLAY" | json_get 'len(timeline)')
[ "$EVENTS" -ge 3 ] && pass "Replay has $EVENTS events" || fail "Expected >= 3 events, got $EVENTS"

# 8. Dashboard
echo "Fetching dashboard..."
METRICS=$(api_get "/v1/metrics/summary")
TOTAL=$(echo "$METRICS" | json_get 'total_requests')
[ "$TOTAL" -ge 2 ] && pass "Dashboard: $TOTAL total requests" || fail "Expected >= 2, got $TOTAL"

# 9. Delete policy
echo "Deleting policy..."
STATUS=$(api_delete "/v1/policies/$POLICY_ID")
assert_status "$STATUS" "204" "delete policy"
pass "Policy deleted"

echo ""
green "=== All smoke tests passed ==="
