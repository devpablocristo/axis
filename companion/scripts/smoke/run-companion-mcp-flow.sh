#!/usr/bin/env bash
# Smoke: MCP -> Companion -> Nexus governance.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../lib/common.sh
source "$SCRIPT_DIR/../lib/common.sh"

AXIS_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

echo "=== Smoke: Companion MCP governed tools ==="

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

echo "Initializing MCP session..."
INIT=$(mcp_rpc '{"jsonrpc":"2.0","id":"init","method":"initialize","params":{"protocolVersion":"2025-11-25","clientInfo":{"name":"axis-smoke","version":"1.0.0"},"capabilities":{}}}')
PROTO=$(printf '%s' "$INIT" | json_get 'result.protocolVersion')
[ "$PROTO" = "2025-11-25" ] && pass "MCP initialize protocolVersion=$PROTO" || fail "Unexpected MCP protocolVersion=$PROTO"

echo "Listing MCP tools via JSON-RPC..."
TOOLS_RPC=$(mcp_rpc '{"jsonrpc":"2.0","id":"tools","method":"tools/list","params":{}}')
if printf '%s' "$TOOLS_RPC" | python3 -c "
import json, sys
tools = json.load(sys.stdin).get('result', {}).get('tools') or []
names = {item.get('name') for item in tools}
required = {'axis.products.list', 'axis.evals.run', 'axis.tasks.create'}
missing = required - names
if missing:
    print('missing ' + ','.join(sorted(missing)), file=sys.stderr)
    raise SystemExit(1)
"; then
  pass "MCP tools/list exposes governed tools"
else
  fail "MCP tools/list missing expected tools"
fi

echo "Listing MCP tools via REST..."
TOOLS_REST=$(companion_get "/v1/mcp/tools")
REST_COUNT=$(printf '%s' "$TOOLS_REST" | json_get 'len(tools)')
[ "$REST_COUNT" -ge 3 ] && pass "REST tool list returns $REST_COUNT tools" || fail "Expected >=3 MCP tools, got $REST_COUNT"

echo "Calling read tool allowed by Nexus..."
ALLOW_IDEM="mcp-smoke-products-$(date +%s)"
ALLOW_BODY=$(mcp_call_body "call-products" "axis.products.list" '{"product_surface":"companion","limit":5}' "$ALLOW_IDEM")
ALLOW_RESP=$(mcp_rpc "$ALLOW_BODY")
ALLOW_STATUS=$(printf '%s' "$ALLOW_RESP" | json_get 'result.structuredContent.status')
ALLOW_REQ=$(printf '%s' "$ALLOW_RESP" | json_get 'result.structuredContent.request_id')
ALLOW_NEXUS=$(printf '%s' "$ALLOW_RESP" | json_get 'result.structuredContent.nexus_status')
[ "$ALLOW_STATUS" = "executed" ] && pass "axis.products.list executed" || fail "Expected executed, got $ALLOW_STATUS"
[ "$ALLOW_NEXUS" = "allowed" ] && pass "Nexus allowed read tool" || fail "Expected Nexus allowed, got $ALLOW_NEXUS"
[ -n "$ALLOW_REQ" ] && pass "Read tool has Nexus request $ALLOW_REQ" || fail "Read tool missing Nexus request id"

REQ=$(api_get "/v1/requests/$ALLOW_REQ")
REQ_TARGET_SYSTEM=$(printf '%s' "$REQ" | json_get 'target_system')
REQ_TARGET_RESOURCE=$(printf '%s' "$REQ" | json_get 'target_resource')
[ "$REQ_TARGET_SYSTEM" = "axis.mcp" ] && pass "Nexus request target_system=axis.mcp" || fail "Unexpected target_system=$REQ_TARGET_SYSTEM"
[ "$REQ_TARGET_RESOURCE" = "axis.products.list" ] && pass "Nexus request target_resource=axis.products.list" || fail "Unexpected target_resource=$REQ_TARGET_RESOURCE"

echo "Calling approval-required eval tool..."
EVAL_IDEM="mcp-smoke-eval-$(date +%s)"
EVAL_BODY=$(mcp_call_body "call-eval" "axis.evals.run" '{"product_surface":"companion","suite":"security-adversarial"}' "$EVAL_IDEM")
EVAL_RESP=$(mcp_rpc "$EVAL_BODY")
EVAL_STATUS=$(printf '%s' "$EVAL_RESP" | json_get 'result.structuredContent.status')
EVAL_NEXUS=$(printf '%s' "$EVAL_RESP" | json_get 'result.structuredContent.nexus_status')
EVAL_REQ=$(printf '%s' "$EVAL_RESP" | json_get 'result.structuredContent.request_id')
[ "$EVAL_STATUS" = "pending_approval" ] && pass "axis.evals.run is pending approval" || fail "Expected pending_approval, got $EVAL_STATUS"
[ "$EVAL_NEXUS" = "pending_approval" ] && pass "Nexus returned pending_approval" || fail "Expected Nexus pending_approval, got $EVAL_NEXUS"
[ -n "$EVAL_REQ" ] && pass "Eval tool has Nexus request $EVAL_REQ" || fail "Eval tool missing Nexus request id"

echo "Calling denied write tool..."
DENY_TITLE="mcp-denied-$(date +%s)"
DENY_IDEM="mcp-smoke-deny-$(date +%s)"
DENY_ARGS=$(python3 - "$DENY_TITLE" <<'PY'
import json, sys
print(json.dumps({"product_surface": "companion", "title": sys.argv[1], "goal": "should not be created"}))
PY
)
DENY_BODY=$(mcp_call_body "call-deny" "axis.tasks.create" "$DENY_ARGS" "$DENY_IDEM")
DENY_RESP=$(mcp_rpc "$DENY_BODY")
DENY_STATUS=$(printf '%s' "$DENY_RESP" | json_get 'result.structuredContent.status')
DENY_IS_ERROR=$(printf '%s' "$DENY_RESP" | json_get 'result.isError')
DENY_REQ=$(printf '%s' "$DENY_RESP" | json_get 'result.structuredContent.request_id')
[ "$DENY_STATUS" = "denied" ] && pass "axis.tasks.create denied by Nexus" || fail "Expected denied, got $DENY_STATUS"
[ "$DENY_IS_ERROR" = "True" ] && pass "Denied MCP call is marked as tool error" || fail "Expected isError true, got $DENY_IS_ERROR"
[ -n "$DENY_REQ" ] && pass "Denied tool has Nexus request $DENY_REQ" || fail "Denied tool missing Nexus request id"

TASKS=$(companion_get "/v1/tasks?limit=200")
if printf '%s' "$TASKS" | python3 -c "
import json, sys
title = sys.argv[1]
items = json.load(sys.stdin).get('data') or []
found = any(item.get('title') == title for item in items)
raise SystemExit(1 if found else 0)
" "$DENY_TITLE"; then
  pass "Denied MCP task was not created"
else
  fail "Denied MCP task was created"
fi

echo "Verifying MCP observability audit..."
AUDIT_ORG_ID="${AXIS_DEV_ORG_ID:-local-dev-org}"
EVENTS=$(companion_get "/v1/observability/events?org_id=${AUDIT_ORG_ID}&product_surface=companion&event_type=mcp&event_name=mcp_tool_call&limit=50")
if EVENTS_JSON="$EVENTS" python3 - "$ALLOW_REQ" "$EVAL_REQ" "$DENY_REQ" <<'PY'
import json, os, sys

required = {value for value in sys.argv[1:] if value}
events = json.loads(os.environ["EVENTS_JSON"]).get("events") or []
seen = set()
for event in events:
    payload = event.get("payload") or {}
    request_id = payload.get("request_id")
    if request_id:
        seen.add(request_id)
missing = required - seen
if missing:
    print("missing audit events for " + ", ".join(sorted(missing)), file=sys.stderr)
    raise SystemExit(1)
PY
then
  pass "MCP tool calls are recorded in observability"
else
  fail "MCP observability audit missing expected tool calls"
fi

echo ""
green "=== Companion MCP smoke passed ==="
