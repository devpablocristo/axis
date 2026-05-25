#!/usr/bin/env bash
# E2E: governed policy promotion with Separation of Duties.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../lib/common.sh"

POLICY_ID=""
cleanup() {
  if [ -n "$POLICY_ID" ]; then
    api_delete_as "$API_KEY_ADMIN_A" "/v1/policies/$POLICY_ID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

version_id_for_number() {
  local policy_id="$1"
  local version="$2"
  api_get_as "$API_KEY_ADMIN_A" "/v1/policies/$policy_id/versions" | python3 -c '
import json, sys
want = int(sys.argv[1])
for item in json.load(sys.stdin).get("data", []):
    if int(item.get("version", 0)) == want:
        print(item["id"])
        raise SystemExit(0)
raise SystemExit(1)
' "$version"
}

assert_changelog_event() {
  local changelog="$1"
  local action="$2"
  local actor="$3"
  echo "$changelog" | python3 -c '
import json, sys
action, actor = sys.argv[1], sys.argv[2]
for item in json.load(sys.stdin).get("data", []):
    data = item.get("data") or {}
    if item.get("action") == action and item.get("actor_id") == actor and data.get("actor_id") == actor:
        raise SystemExit(0)
raise SystemExit(1)
' "$action" "$actor"
}

assert_changelog_approval() {
  local changelog="$1"
  echo "$changelog" | python3 -c '
import json, sys
for item in json.load(sys.stdin).get("data", []):
    data = item.get("data") or {}
    if item.get("action") == "promotion_approved" and data.get("requested_by") == "nexus-admin-a" and data.get("approved_by") == "nexus-admin-b":
        raise SystemExit(0)
raise SystemExit(1)
'
}

echo "=== E2E: policy promotion SoD ==="

wait_for_http "$API_BASE/readyz"

RUN_ID="$(date +%s)-$$"
ACTION_TYPE="alert.escalate"
TARGET_RESOURCE="policy-promotion-sod-$RUN_ID"
POLICY_NAME="e2e-policy-promotion-sod-$RUN_ID"
EXPR="request.action_type == '$ACTION_TYPE' && request.target_resource == '$TARGET_RESOURCE'"

CREATE_BODY=$(printf '{"name":%s,"expression":%s,"effect":"allow","priority":1,"enabled":true}' "$(json_string "$POLICY_NAME")" "$(json_string "$EXPR")")
POLICY=$(api_post_as "$API_KEY_ADMIN_A" "/v1/policies" "$CREATE_BODY")
POLICY_ID=$(echo "$POLICY" | json_get 'id')
pass "Setup: Admin A created policy"

api_patch_as "$API_KEY_ADMIN_A" "/v1/policies/$POLICY_ID" '{"effect":"deny"}' >/dev/null
TARGET_VERSION_ID=$(version_id_for_number "$POLICY_ID" 2)
api_patch_as "$API_KEY_ADMIN_A" "/v1/policies/$POLICY_ID" '{"effect":"allow"}' >/dev/null
pass "Setup: target deny version prepared without DB hacks"

PROMOTION_BODY=$(printf '{"to_version_id":"%s","reason":"E2E SoD promotion","dry_run_report":{"status":"passed","run_id":%s}}' "$TARGET_VERSION_ID" "$(json_string "$RUN_ID")")
PROMOTION=$(api_post_as "$API_KEY_ADMIN_A" "/v1/policies/$POLICY_ID/promotions" "$PROMOTION_BODY")
PROMOTION_ID=$(echo "$PROMOTION" | json_get 'id')
REQUESTED_BY=$(echo "$PROMOTION" | json_get 'requested_by')
[ "$REQUESTED_BY" = "nexus-admin-a" ] && pass "1. Admin A requested promotion" || fail "expected requested_by nexus-admin-a, got $REQUESTED_BY"

STATUS=$(api_status_as "$API_KEY_ADMIN_A" POST "/v1/policy-promotions/$PROMOTION_ID/approve" '{}')
assert_status "$STATUS" "409" "self approval"
pass "2. Self-approval rejected with 409"

STATUS=$(api_status_as "$API_KEY_OTHER_ORG" POST "/v1/policy-promotions/$PROMOTION_ID/approve" '{}')
assert_status "$STATUS" "403" "cross-org approval"
pass "3. Other org admin cannot approve"

STATUS=$(api_status_as "$API_KEY_OTHER_ORG" POST "/v1/policy-promotions/$PROMOTION_ID/enforce" '{}')
assert_status "$STATUS" "403" "cross-org enforce"
pass "4. Other org admin cannot enforce"

STATUS=$(api_status_as "$API_KEY_ADMIN_B" POST "/v1/policy-promotions/$PROMOTION_ID/enforce" '{}')
assert_status "$STATUS" "409" "enforce before approval"
pass "5. Enforce before approval rejected"

APPROVED=$(api_post_as "$API_KEY_ADMIN_B" "/v1/policy-promotions/$PROMOTION_ID/approve" '{}')
APPROVED_STATUS=$(echo "$APPROVED" | json_get 'status')
APPROVED_BY=$(echo "$APPROVED" | json_get 'approved_by')
[ "$APPROVED_STATUS" = "approved" ] && [ "$APPROVED_BY" = "nexus-admin-b" ] && pass "6. Admin B approved promotion" || fail "approval state mismatch"

STATUS=$(api_status_as "$API_KEY_ADMIN_B" POST "/v1/policy-promotions/$PROMOTION_ID/approve" '{}')
assert_status "$STATUS" "409" "approve twice"
pass "7. Duplicate approval rejected"

ENFORCED=$(api_post_as "$API_KEY_ADMIN_B" "/v1/policy-promotions/$PROMOTION_ID/enforce" '{}')
ENFORCED_STATUS=$(echo "$ENFORCED" | json_get 'status')
ENFORCED_BY=$(echo "$ENFORCED" | json_get 'enforced_by')
[ "$ENFORCED_STATUS" = "enforced" ] && [ "$ENFORCED_BY" = "nexus-admin-b" ] && pass "8. Approved promotion enforced" || fail "enforce state mismatch"

STATUS=$(api_status_as "$API_KEY_ADMIN_B" POST "/v1/policy-promotions/$PROMOTION_ID/enforce" '{}')
assert_status "$STATUS" "409" "enforce twice"
pass "9. Duplicate enforce rejected"

REQUEST_BODY=$(printf '{"requester_type":"agent","requester_id":"policy-promotion-sod-e2e","action_type":%s,"target_system":"internal","target_resource":%s,"reason":"prove enforced policy"}' "$(json_string "$ACTION_TYPE")" "$(json_string "$TARGET_RESOURCE")")
REQUEST=$(api_post_as "$API_KEY_ADMIN_A" "/v1/requests" "$REQUEST_BODY")
DECISION=$(echo "$REQUEST" | json_get 'decision')
[ "$DECISION" = "deny" ] && pass "10. Governed request proves enforced policy is active" || fail "expected deny after promotion, got $DECISION"

CHANGELOG=$(api_get_as "$API_KEY_ADMIN_A" "/v1/policies/$POLICY_ID/changelog")
assert_changelog_event "$CHANGELOG" "promotion_requested" "nexus-admin-a" && pass "11. Audit has requester"
assert_changelog_event "$CHANGELOG" "promotion_approval_denied" "nexus-admin-a" && pass "12. Audit has SoD denial"
assert_changelog_event "$CHANGELOG" "promotion_approved" "nexus-admin-b" && pass "13. Audit has approver"
assert_changelog_event "$CHANGELOG" "promotion_enforced" "nexus-admin-b" && pass "14. Audit has enforcer"
assert_changelog_approval "$CHANGELOG" && pass "15. Audit links requester and approver"

STATUS=$(api_status_as "$API_KEY_OTHER_ORG" GET "/v1/policies/$POLICY_ID/changelog")
assert_status "$STATUS" "403" "cross-org changelog"
pass "16. Changelog is tenant-isolated"

echo ""
green "=== Policy promotion SoD E2E passed ==="
