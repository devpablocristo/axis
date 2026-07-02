#!/usr/bin/env bash
# Seed idempotente de action_types + policies que consume ponti-backend.
# Se ejecuta despues de `up-ponti-local` contra el Nexus local.

set -euo pipefail

BASE="${NEXUS_BASE_URL:-http://localhost:18084}"
ADMIN_KEY="${NEXUS_ADMIN_API_KEY:-nexus-admin-dev-key}"
PONTI_KEY="${NEXUS_PONTI_API_KEY:-nexus-ponti-dev-key}"

echo "Seed Nexus en $BASE"
echo ""

wait_for_ready() {
  for i in {1..40}; do
    if curl -fsS "$BASE/readyz" >/dev/null 2>&1; then
      echo "Nexus listo."
      return 0
    fi
    sleep 1
  done
  echo "ERROR: Nexus no respondio en 40s" >&2
  exit 1
}

ensure_action_type() {
  local name="$1"
  local description="$2"
  local risk="$3"
  local existing
  existing="$(curl -fsS "$BASE/v1/action-types" -H "X-API-Key: $ADMIN_KEY" | grep -o "\"name\":\"$name\"" || true)"
  if [[ -n "$existing" ]]; then
    echo "  action_type $name ya existe, skip."
    return
  fi
  echo "  creando action_type $name..."
  curl -fsS -X POST "$BASE/v1/action-types" \
    -H "X-API-Key: $ADMIN_KEY" \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"$name\",\"description\":\"$description\",\"category\":\"ponti\",\"risk_class\":\"$risk\",\"reversible\":true,\"requires_break_glass\":false}" \
    >/dev/null
  echo "  OK action_type $name."
}

ensure_policy() {
  local name="$1"
  local action_type="$2"
  local expression="$3"
  local effect="$4"
  local existing
  existing="$(curl -fsS "$BASE/v1/policies" -H "X-API-Key: $ADMIN_KEY" | grep -o "\"name\":\"$name\"" || true)"
  if [[ -n "$existing" ]]; then
    echo "  policy $name ya existe, skip."
    return
  fi
  echo "  creando policy $name..."
  curl -fsS -X POST "$BASE/v1/policies" \
    -H "X-API-Key: $ADMIN_KEY" \
    -H "Content-Type: application/json" \
    -d "$(cat <<JSON
{
  "name": "$name",
  "description": "Auto-seeded from make up-ponti-local",
  "expression": $(printf '%s' "$expression" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))'),
  "effect": "$effect",
  "action_type": "$action_type",
  "target_system": "ponti",
  "enabled": true,
  "mode": "enforced",
  "priority": 100
}
JSON
)" \
    >/dev/null
  echo "  OK policy $name."
}

wait_for_ready

echo "== action_types =="
ensure_action_type "ponti.stock.negative" "Stock de insumo quedo negativo" "low"

echo "== policies =="
ensure_policy \
  "ponti-stock-negative-notify" \
  "ponti.stock.negative" \
  'request.action_type == "ponti.stock.negative" && request.target_system == "ponti" && double(request.params.quantity) < 0.0' \
  "allow"

echo ""
echo "Seed completado. Ponti key para usar desde ponti-backend: $PONTI_KEY"
