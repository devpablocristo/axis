#!/usr/bin/env bash
set -euo pipefail

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

require_env() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    echo "missing required env var: $name" >&2
    exit 1
  fi
}

api() {
  local method="$1"
  local path="$2"
  local data="${3:-}"
  local url="${COMPANION_BASE_URL}${path}"
  if [[ -n "$data" ]]; then
    curl -fsS -X "$method" "$url" \
      -H "Content-Type: application/json" \
      -H "X-API-Key: ${COMPANION_API_KEY}" \
      --data "$data"
    return
  fi
  curl -fsS -X "$method" "$url" -H "X-API-Key: ${COMPANION_API_KEY}"
}

need curl
need jq

COMPANION_BASE_URL="${COMPANION_BASE_URL:-${AXIS_COMPANION_BASE_URL:-http://localhost:18085}}"
COMPANION_API_KEY="${COMPANION_API_KEY:-${AXIS_COMPANION_API_KEY:-companion-admin-dev-key}}"
PRODUCT_AUTH_MODE="${PRODUCT_AUTH_MODE:-api_key_ref}"
PRODUCT_DISCOVERY_PATH="${PRODUCT_DISCOVERY_PATH:-/api/v1/capabilities}"
PRODUCT_EXECUTE_PATH="${PRODUCT_EXECUTE_PATH:-/api/v1/capability-executions}"

require_env PRODUCT_SURFACE
require_env PRODUCT_ORG_ID
require_env PRODUCT_BASE_URL
if [[ "$PRODUCT_AUTH_MODE" == "api_key_ref" || "$PRODUCT_AUTH_MODE" == "oauth2" || "$PRODUCT_AUTH_MODE" == "custom" ]]; then
  require_env PRODUCT_SECRET_REF
fi

PRODUCT_DISPLAY_NAME="${PRODUCT_DISPLAY_NAME:-$PRODUCT_SURFACE}"
PRODUCT_EXTERNAL_TENANT_ID="${PRODUCT_EXTERNAL_TENANT_ID:-$PRODUCT_ORG_ID}"

product_payload="$(jq -n \
  --arg display_name "$PRODUCT_DISPLAY_NAME" \
  --arg surface "$PRODUCT_SURFACE" \
  --arg base_url "$PRODUCT_BASE_URL" \
  '{
    display_name: $display_name,
    status: "active",
    metadata: {
      onboarded_by: "seed-product-installation.sh",
      product_surface: $surface,
      capabilities_url: ($base_url + "/api/v1/capabilities"),
      contract: "capability_execution.v1"
    }
  }')"

installation_payload="$(jq -n \
  --arg external_tenant_id "$PRODUCT_EXTERNAL_TENANT_ID" \
  --arg base_url "$PRODUCT_BASE_URL" \
  --arg auth_mode "$PRODUCT_AUTH_MODE" \
  --arg secret_ref "${PRODUCT_SECRET_REF:-}" \
  --arg discovery_path "$PRODUCT_DISCOVERY_PATH" \
  --arg execute_path "$PRODUCT_EXECUTE_PATH" \
  '{
    external_tenant_id: $external_tenant_id,
    base_url: $base_url,
    auth_mode: $auth_mode,
    secret_ref: $secret_ref,
    enabled: true,
    config: {
      connector_mode: "envelope.v1",
      discovery_path: $discovery_path,
      execute_path: $execute_path
    }
  }')"

echo "Registering product: $PRODUCT_SURFACE"
api PUT "/v1/products/${PRODUCT_SURFACE}" "$product_payload" >/dev/null

echo "Registering installation: ${PRODUCT_ORG_ID} + ${PRODUCT_SURFACE}"
api PUT "/v1/product-installations/${PRODUCT_SURFACE}?org_id=${PRODUCT_ORG_ID}" "$installation_payload" >/dev/null

echo "Refreshing connectors"
api POST "/v1/connectors/refresh" '{}' >/dev/null || true

cat <<EOF
Product installation seeded.
product_surface=$PRODUCT_SURFACE
org_id=$PRODUCT_ORG_ID
external_tenant_id=$PRODUCT_EXTERNAL_TENANT_ID
base_url=$PRODUCT_BASE_URL
connector_mode=envelope.v1
EOF
