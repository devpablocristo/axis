#!/usr/bin/env bash
# Seed idempotente de policies Nexus para regular tools MCP de Axis.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib/common.sh
source "$SCRIPT_DIR/lib/common.sh"

echo "=== Seed: Axis MCP Nexus policies ==="

wait_for_http "$API_BASE/readyz"

policy_id_by_name() {
  local name="$1"
  api_get "/v1/policies" | python3 -c "
import json, sys
name = sys.argv[1]
items = json.load(sys.stdin).get('data') or []
match = next((item for item in items if item.get('name') == name and not item.get('archived_at')), None)
print(match.get('id', '') if match else '')
" "$name"
}

policy_body() {
  local name="$1"
  local tool="$2"
  local effect="$3"
  local priority="$4"
  python3 - "$name" "$tool" "$effect" "$priority" <<'PY'
import json, sys
name, tool, effect, priority = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4])
print(json.dumps({
    "name": name,
    "description": f"Seeded local policy for Axis MCP tool {tool}",
    "action_type": "agent.capability.invoke",
    "target_system": "axis.mcp",
    "expression": (
        "request.action_type == 'agent.capability.invoke' && "
        "request.target_system == 'axis.mcp' && "
        f"request.target_resource == '{tool}'"
    ),
    "effect": effect,
    "priority": priority,
    "enabled": True,
}))
PY
}

ensure_policy() {
  local name="$1"
  local tool="$2"
  local effect="$3"
  local priority="$4"
  local body id created
  body="$(policy_body "$name" "$tool" "$effect" "$priority")"
  id="$(policy_id_by_name "$name")"
  if [ -n "$id" ]; then
    api_patch_as "$API_KEY" "/v1/policies/$id" "$body" >/dev/null
    pass "Updated policy $name ($id)"
    return 0
  fi
  created="$(api_post "/v1/policies" "$body")"
  id="$(printf '%s' "$created" | json_get 'id')"
  [ -n "$id" ] && pass "Created policy $name ($id)" || fail "Policy $name id missing"
}

ensure_policy "axis-mcp-allow-products-list" "axis.products.list" "allow" 1
ensure_policy "axis-mcp-require-approval-evals-run" "axis.evals.run" "require_approval" 1
ensure_policy "axis-mcp-deny-tasks-create" "axis.tasks.create" "deny" 1

echo ""
green "=== Axis MCP policies ready ==="
