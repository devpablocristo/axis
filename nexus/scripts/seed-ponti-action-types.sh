#!/usr/bin/env bash
# Seed idempotente de action_types + policies base para las acciones gobernadas
# de Ponti (Ola A.3). Los schemas de params copian los input schemas de los
# draft tools de ponti/core internal/ai/capabilities.go.
#
# Delegations: no se seedean. El checker de Nexus trata "requester sin
# delegaciones registradas" como sin restricciones (compat PoC), así que
# ponti-backend puede submitear estos action types sin delegation explícita.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib/common.sh
source "$SCRIPT_DIR/lib/common.sh"

echo "=== Seed: Ponti action types + base policies ==="

wait_for_http "$API_BASE/readyz"

action_type_id_by_name() {
  local name="$1"
  api_get "/v1/action-types" | python3 -c "
import json, sys
name = sys.argv[1]
items = json.load(sys.stdin).get('data') or []
match = next((item for item in items if item.get('name') == name), None)
print(match.get('id', '') if match else '')
" "$name"
}

action_type_body() {
  local name="$1"
  local description="$2"
  local risk="$3"
  local reversible="$4"
  python3 - "$name" "$description" "$risk" "$reversible" <<'PY'
import json, sys

name, description, risk, reversible = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4] == "true"

# Copia de workspaceSchema() de ponti/core internal/ai/capabilities.go.
WORKSPACE = {
    "type": "object",
    "properties": {
        "customer_id": {"type": "integer", "minimum": 1},
        "project_id": {"type": "integer", "minimum": 1},
        "campaign_id": {"type": "integer", "minimum": 1},
        "field_id": {"type": "integer", "minimum": 1},
    },
}

# Input schemas copiados de los draft tools de ponti/core
# internal/ai/capabilities.go (workorder_draft.create, insight_resolution.draft,
# stock_adjustment.prepare, stock_count.draft).
SCHEMAS = {
    "ponti.workorder.draft.create": {
        "type": "object",
        "properties": {
            "number": {"type": "string"},
            "date": {"type": "string", "format": "date"},
            "customer_id": {"type": "integer", "minimum": 1},
            "project_id": {"type": "integer", "minimum": 1},
            "campaign_id": {"type": "integer", "minimum": 1},
            "field_id": {"type": "integer", "minimum": 1},
            "lot_id": {"type": "integer", "minimum": 1},
            "crop_id": {"type": "integer", "minimum": 1},
            "labor_id": {"type": "integer", "minimum": 1},
            "contractor": {"type": "string", "minLength": 1},
            "effective_area": {"type": "number"},
            "observations": {"type": "string", "maxLength": 2000},
            "investor_id": {"type": "integer", "minimum": 1},
            "items": {
                "type": "array",
                "items": {
                    "type": "object",
                    "properties": {
                        "supply_id": {"type": "integer", "minimum": 1},
                        "total_used": {"type": "number"},
                        "final_dose": {"type": "number"},
                    },
                    "required": ["supply_id", "total_used", "final_dose"],
                },
            },
            "workspace": WORKSPACE,
        },
        "required": [
            "date", "customer_id", "project_id", "field_id", "lot_id",
            "crop_id", "labor_id", "contractor", "effective_area", "workspace",
        ],
    },
    "ponti.insight.resolve": {
        "type": "object",
        "properties": {
            "insight_id": {"type": "string", "format": "uuid"},
            "resolution_note": {"type": "string", "maxLength": 1000},
            "workspace": WORKSPACE,
        },
        "required": ["insight_id", "workspace"],
    },
    "ponti.stock.adjust": {
        "type": "object",
        "properties": {
            "project_id": {"type": "integer", "minimum": 1},
            "supply_id": {"type": "integer", "minimum": 1},
            "quantity_delta": {"type": "number"},
            "reason": {"type": "string", "minLength": 1, "maxLength": 1000},
            "workspace": WORKSPACE,
        },
        "required": ["project_id", "supply_id", "quantity_delta", "reason"],
    },
    "ponti.stock.count.apply": {
        "type": "object",
        "properties": {
            "project_id": {"type": "integer", "minimum": 1},
            "stock_id": {"type": "integer", "minimum": 1},
            "supply_id": {"type": "integer", "minimum": 1},
            "real_stock_units": {"type": "number"},
            "reason": {"type": "string", "minLength": 1, "maxLength": 1000},
            "workspace": WORKSPACE,
        },
        "required": ["project_id", "supply_id", "real_stock_units", "reason", "workspace"],
    },
}

print(json.dumps({
    "name": name,
    "description": description,
    "category": "ponti",
    "risk_class": risk,
    "schema": SCHEMAS[name],
    "reversible": reversible,
    "requires_break_glass": False,
}))
PY
}

ensure_action_type() {
  local name="$1"
  local description="$2"
  local risk="$3"
  local reversible="$4"
  local body id created
  body="$(action_type_body "$name" "$description" "$risk" "$reversible")"
  id="$(action_type_id_by_name "$name")"
  if [ -n "$id" ]; then
    api_patch_as "$API_KEY" "/v1/action-types/$id" "$body" >/dev/null
    pass "Updated action_type $name ($id)"
    return 0
  fi
  created="$(api_post "/v1/action-types" "$body")"
  id="$(printf '%s' "$created" | json_get 'id')"
  [ -n "$id" ] && pass "Created action_type $name ($id)" || fail "Action type $name id missing"
}

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
  local action_type="$2"
  local mode="$3"
  python3 - "$name" "$action_type" "$mode" <<'PY'
import json, sys
name, action_type, mode = sys.argv[1], sys.argv[2], sys.argv[3]
print(json.dumps({
    "name": name,
    "description": f"Seeded base policy for Ponti action {action_type}",
    "action_type": action_type,
    "target_system": "ponti",
    "expression": (
        f"request.action_type == '{action_type}' && "
        "request.target_system == 'ponti'"
    ),
    "effect": "require_approval",
    "priority": 100,
    "mode": mode,
    "enabled": True,
}))
PY
}

ensure_policy() {
  local name="$1"
  local action_type="$2"
  local mode="$3"
  local body id created
  body="$(policy_body "$name" "$action_type" "$mode")"
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

echo ""
echo "== action_types =="
ensure_action_type "ponti.workorder.draft.create" "Crea un borrador digital de orden de trabajo en Ponti" "medium" "true"
ensure_action_type "ponti.insight.resolve" "Resuelve un insight de Ponti con nota reversible" "medium" "true"
ensure_action_type "ponti.stock.adjust" "Ajusta inventario de un insumo en Ponti" "high" "false"
ensure_action_type "ponti.stock.count.apply" "Aplica un conteo físico de stock en Ponti" "high" "false"

echo ""
echo "== policies =="
# medium → require_approval en shadow (medir antes de enforcear).
ensure_policy "ponti-workorder-draft-create-require-approval" "ponti.workorder.draft.create" "shadow"
ensure_policy "ponti-insight-resolve-require-approval" "ponti.insight.resolve" "shadow"
# high → require_approval enforced.
ensure_policy "ponti-stock-adjust-require-approval" "ponti.stock.adjust" "enforced"
ensure_policy "ponti-stock-count-apply-require-approval" "ponti.stock.count.apply" "enforced"

echo ""
green "=== Ponti action types + policies ready ==="
