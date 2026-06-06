#!/usr/bin/env bash
# Smoke: MCP runtime policy admin -> MCP enforcement -> observability/ops.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../lib/common.sh
source "$SCRIPT_DIR/../lib/common.sh"

AXIS_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
AUDIT_ORG_ID="${AXIS_DEV_ORG_ID:-local-dev-org}"

echo "=== Smoke: Companion MCP runtime policy enforcement ==="

wait_for_http "$API_BASE/readyz"
wait_for_http "$COMPANION_BASE/readyz"
pass "Nexus and Companion are up"

echo "Seeding Axis MCP policies in Nexus..."
API_BASE="$API_BASE" API_KEY="$API_KEY" bash "$AXIS_ROOT/nexus/scripts/seed-axis-mcp-policies.sh"
pass "Axis MCP policies are ready"

mcp_rpc() {
  companion_post "/mcp" "$1"
}

mcp_call_body() {
  local id="$1"
  local tool="$2"
  local args_json="$3"
  local idem="$4"
  python3 - "$id" "$tool" "$args_json" "$idem" <<'PY'
import json, sys
id_, tool, args_raw, idem = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
print(json.dumps({
    "jsonrpc": "2.0",
    "id": id_,
    "method": "tools/call",
    "params": {
        "name": tool,
        "arguments": json.loads(args_raw),
        "idempotency_key": idem,
    },
}))
PY
}

reset_mcp_policy() {
  companion_put "/v1/runtime/mcp-policy" '{
    "enabled": true,
    "kill_switch": false,
    "allowed_product_surfaces": [],
    "allowed_tools": [],
    "denied_tools": [],
    "tool_kill_switches": {},
    "product_policies": {
      "companion": {"denied": false}
    },
    "metadata": {
      "changed_by": "axis.mcp.runtime_policy_smoke",
      "change_reason": "reset runtime policy smoke state"
    }
  }' > /dev/null
}

cleanup() {
  set +e
  reset_mcp_policy
}
trap cleanup EXIT

runtime_policy_guardrail_count() {
  local events
  events=$(companion_get "/v1/observability/events?org_id=${AUDIT_ORG_ID}&product_surface=companion&limit=500")
  EVENTS_JSON="$events" python3 - <<'PY'
import json, os

events = json.loads(os.environ["EVENTS_JSON"]).get("events") or []
count = 0
for event in events:
    if event.get("event_type") != "guardrail" or event.get("event_name") != "mcp_runtime_policy":
        continue
    payload = event.get("payload") or {}
    if payload.get("tool_name") == "axis.products.list" and payload.get("target") == "tool:axis.products.list":
        count += 1
print(count)
PY
}

echo "Resetting MCP runtime policy..."
reset_mcp_policy
pass "MCP runtime policy reset"

echo "Denying axis.products.list via MCP runtime policy..."
companion_put "/v1/runtime/mcp-policy" '{
  "denied_tools": ["axis.products.list"],
  "metadata": {
    "changed_by": "axis.mcp.runtime_policy_smoke",
    "change_reason": "deny products list for smoke"
  }
}' > /dev/null
pass "MCP runtime policy denied axis.products.list"

BEFORE_BLOCK_EVENTS=$(runtime_policy_guardrail_count)
BLOCK_IDEM="mcp-runtime-policy-block-$(date +%s)"
BLOCK_BODY=$(mcp_call_body "call-runtime-block" "axis.products.list" '{"product_surface":"companion","limit":5}' "$BLOCK_IDEM")
BLOCK_RESP=$(mcp_rpc "$BLOCK_BODY")

if BLOCK_RESP_JSON="$BLOCK_RESP" python3 - <<'PY'
import json, os, sys

response = json.loads(os.environ["BLOCK_RESP_JSON"])
error = response.get("error") or {}
data = error.get("data") or {}
metadata = data.get("metadata") or {}
checks = [
    (data.get("status") == 403, "expected error.data.status=403"),
    (data.get("mcp_status") == "blocked", "expected error.data.mcp_status=blocked"),
    (metadata.get("blocked_by") == "runtime_policy", "expected blocked_by=runtime_policy"),
    (not data.get("request_id") and not metadata.get("request_id"), "runtime policy block must not include Nexus request_id"),
]
failed = [message for ok, message in checks if not ok]
if failed:
    print("; ".join(failed), file=sys.stderr)
    print(json.dumps(response, indent=2), file=sys.stderr)
    raise SystemExit(1)
PY
then
  pass "Runtime policy blocked MCP call before Nexus"
else
  fail "Runtime policy block response was not a 403 MCP blocked error"
fi

AFTER_BLOCK_EVENTS=$(runtime_policy_guardrail_count)
if [ "$AFTER_BLOCK_EVENTS" -gt "$BEFORE_BLOCK_EVENTS" ]; then
  pass "Runtime policy guardrail event was recorded"
else
  fail "Expected a new mcp_runtime_policy guardrail event"
fi

echo "Verifying MCP runtime policy alert..."
ALERTS=$(companion_get "/v1/ops/alerts?org_id=${AUDIT_ORG_ID}&product_surface=companion&limit=100")
if ALERTS_JSON="$ALERTS" python3 - <<'PY'
import json, os, sys

alerts = json.loads(os.environ["ALERTS_JSON"]).get("alerts") or []
for alert in alerts:
    if alert.get("type") != "mcp_runtime_policy_block":
        continue
    evidence = alert.get("evidence") or {}
    if evidence.get("tool_name") == "axis.products.list" and evidence.get("target") == "tool:axis.products.list":
        raise SystemExit(0)
print("mcp_runtime_policy_block alert not found", file=sys.stderr)
raise SystemExit(1)
PY
then
  pass "MCP runtime policy block appears in ops alerts"
else
  fail "MCP runtime policy block alert missing"
fi

echo "Allowing axis.products.* via MCP runtime policy..."
companion_put "/v1/runtime/mcp-policy" '{
  "allowed_tools": ["axis.products.*"],
  "denied_tools": [],
  "metadata": {
    "changed_by": "axis.mcp.runtime_policy_smoke",
    "change_reason": "allow products wildcard for smoke"
  }
}' > /dev/null
pass "MCP runtime policy allows axis.products.*"

ALLOW_IDEM="mcp-runtime-policy-allow-$(date +%s)"
ALLOW_BODY=$(mcp_call_body "call-runtime-allow" "axis.products.list" '{"product_surface":"companion","limit":5}' "$ALLOW_IDEM")
ALLOW_RESP=$(mcp_rpc "$ALLOW_BODY")
ALLOW_STATUS=$(printf '%s' "$ALLOW_RESP" | json_get 'result.structuredContent.status')
ALLOW_NEXUS=$(printf '%s' "$ALLOW_RESP" | json_get 'result.structuredContent.nexus_status')
ALLOW_REQ=$(printf '%s' "$ALLOW_RESP" | json_get 'result.structuredContent.request_id')
[ "$ALLOW_STATUS" = "executed" ] && pass "axis.products.list executed after runtime policy allow" || fail "Expected executed, got $ALLOW_STATUS"
[ "$ALLOW_NEXUS" = "allowed" ] && pass "Nexus allowed after runtime policy allow" || fail "Expected Nexus allowed, got $ALLOW_NEXUS"
[ -n "$ALLOW_REQ" ] && pass "Allowed call has Nexus request $ALLOW_REQ" || fail "Allowed call missing Nexus request id"

echo ""
green "=== Companion MCP runtime policy smoke passed ==="
