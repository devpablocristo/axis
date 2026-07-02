#!/usr/bin/env bash
# Local/staging job trigger for the generic Axis billing_agent.
# It creates one operational agent run that asks the agent to scan pending
# billing plan requests through product capabilities. It does not change tiers.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../lib/common.sh
source "$SCRIPT_DIR/../lib/common.sh"

ORG_ID="${AXIS_BILLING_AGENT_ORG_ID:-local-dev-org}"
PRODUCT_SURFACE="${AXIS_BILLING_AGENT_PRODUCT_SURFACE:-medmory}"

wait_for_http "$COMPANION_BASE/readyz"

BODY=$(cat <<JSON
{
  "agent_id": "billing_agent",
  "product_surface": "$PRODUCT_SURFACE",
  "run_type": "billing.plan_requests.scan",
  "input": {
    "source": "local_staging_job",
    "idempotency_key": "billing_agent.pending_plan_requests.scan:$ORG_ID:$PRODUCT_SURFACE:$(date -u +%Y-%m-%dT%H)"
  }
}
JSON
)

RUN=$(companion_post "/v1/agent-runs?org_id=$ORG_ID&product_surface=$PRODUCT_SURFACE" "$BODY")
RUN_ID=$(printf '%s' "$RUN" | json_get 'id')

pass "billing_agent scan run created: $RUN_ID"
