#!/usr/bin/env bash
set -Eeuo pipefail

BFF_URL="${BFF_URL:-http://127.0.0.1:19080}"
DEV_ACTOR_ID="${DEV_ACTOR_ID:-dev-user}"
DEV_ACTOR_EMAIL="${DEV_ACTOR_EMAIL:-dev@example.local}"
DEV_ORG_ID="${DEV_ORG_ID:-dev-org}"
DEMO_SERVICE_PRINCIPAL="${DEMO_SERVICE_PRINCIPAL:-axis-demo}"

for command in curl jq; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "missing required command: $command" >&2
    exit 1
  fi
done

session="$(
  curl -fsS "$BFF_URL/api/session" \
    -H "X-Actor-ID: $DEV_ACTOR_ID" \
    -H "X-Actor-Email: $DEV_ACTOR_EMAIL" \
    -H "X-Axis-Org-ID: $DEV_ORG_ID"
)"

actor_id="$(jq -r '.principal_id // .actor_id // empty' <<<"$session")"
org_id="$(jq -r '.organizations[0].id // .org_id // empty' <<<"$session")"
product_id="$(jq -r '.organizations[0].products[0].id // empty' <<<"$session")"
product_surface="$(jq -r '.organizations[0].products[0].product_surface // empty' <<<"$session")"
if [[ -z "$actor_id" || -z "$org_id" || -z "$product_id" || -z "$product_surface" ]]; then
  echo "could not resolve the demo organization and product from /api/session" >&2
  exit 1
fi

integration_path="/api/organizations/$org_id/products/$product_id/integration"

api() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  local arguments=(
    --silent
    --show-error
    --fail-with-body
    --request "$method"
    "$BFF_URL$path"
    --header "X-Actor-ID: $actor_id"
    --header "X-Org-ID: $org_id"
    --header "X-Product-Surface: $product_surface"
  )
  if [[ -n "$body" ]]; then
    arguments+=(--header "Content-Type: application/json" --data "$body")
  fi
  curl "${arguments[@]}"
}

contract="$(
  jq -n '{
    schema_version: "axis.product-integration.v3",
    authentication: {
      mode: "api_key",
      scopes: ["assist.read", "assist.write", "events.write"]
    },
    limits: {
      max_request_bytes: 1048576,
      max_result_bytes: 1048576,
      rate_per_minute: 60
    },
    entrypoints: [],
    capabilities: [],
    events: [],
    governed_operations: [],
    connector_bindings: []
  }'
)"

version="$(
  api POST "$integration_path/versions" "$(jq -n --argjson contract "$contract" '{contract: $contract}')"
)"
version_id="$(jq -r '.id // empty' <<<"$version")"
if [[ -z "$version_id" ]]; then
  echo "BFF did not return an integration version id" >&2
  echo "$version" >&2
  exit 1
fi

validation="$(api POST "$integration_path/versions/$version_id/validate")"
if ! jq -e '.valid == true' >/dev/null <<<"$validation"; then
  echo "demo integration validation failed" >&2
  echo "$validation" | jq . >&2
  exit 1
fi
api POST "$integration_path/versions/$version_id/activate" >/dev/null

credentials="$(api GET "$integration_path/credentials")"
credential_id="$(
  jq -r --arg principal "$DEMO_SERVICE_PRINCIPAL" \
    '.items[]? | select(.service_principal == $principal and .status == "active") | .id' \
    <<<"$credentials" | head -n 1
)"

if [[ -n "$credential_id" ]]; then
  cat <<EOF
neutral persisted demo integration is active
org_id=$org_id
product_id=$product_id
integration_version_id=$version_id
credential_id=$credential_id
credential_secret=already-created-and-not-readable
EOF
  exit 0
fi

credential="$(
  api POST "$integration_path/credentials" "$(
    jq -n --arg principal "$DEMO_SERVICE_PRINCIPAL" '{
      service_principal: $principal,
      scopes: ["assist.read", "assist.write", "events.write"]
    }'
  )"
)"

cat <<EOF
neutral persisted demo integration is active
org_id=$org_id
product_id=$product_id
integration_version_id=$version_id
credential_id=$(jq -r '.id' <<<"$credential")
credential_secret=$(jq -r '.secret' <<<"$credential")

The secret is shown once. Add entrypoints, capability UUIDs and connector
bindings through the product integration editor before invoking governed work.
EOF
