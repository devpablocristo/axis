#!/usr/bin/env bash
# Smoke: Companion governance-assist → Nexus learning endpoints roundtrip.
# Valida que companion (proposer/contextualizer) puede hablar contra el nuevo
# /v1/learning/proposals que introdujo el refactor "Nexus 100% AI-independent".
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../lib/common.sh
source "$SCRIPT_DIR/../lib/common.sh"

ORG_ID="${SMOKE_ORG_ID:-local-dev-org}"

# companion_post_with_org reproduce companion_post pero agrega X-Org-ID
# (necesario para governance-assist/propose).
companion_post_with_org() {
  local url="$COMPANION_BASE$1"
  local out code
  out=$(curl -sS -w "\n%{http_code}" -X POST \
    -H "X-API-Key: $COMPANION_API_KEY" \
    -H "X-Org-ID: $ORG_ID" \
    -H "Content-Type: application/json" \
    -d "$2" "$url") || return $?
  code=$(echo "$out" | tail -n1)
  out=$(echo "$out" | sed '$d')
  if ! [[ "$code" =~ ^[0-9]{3}$ ]] || [ "$code" -ge 400 ]; then
    echo "companion POST $1 (org=$ORG_ID) failed: HTTP ${code:-?} $out" >&2
    return 22
  fi
  printf '%s' "$out"
}

echo "=== Smoke: Companion governance-assist flow ==="

wait_for_http "$API_BASE/healthz"
wait_for_http "$COMPANION_BASE/readyz"
pass "Governance and Companion are up"

# --- (a) /v1/governance-assist/propose: analiza histórico Nexus + POSTea ---
echo "Triggering governance-assist analyzer (POST /v1/governance-assist/propose)..."
RESP=$(companion_post_with_org "/v1/governance-assist/propose" '{}')
if echo "$RESP" | python3 -c "
import json, sys
d = json.load(sys.stdin)
required = ('patterns_analyzed', 'proposals_submitted')
ok = all(k in d for k in required)
sys.exit(0 if ok else 1)
"; then
  pass "Response shape OK (patterns_analyzed + proposals_submitted)"
else
  fail "Missing required fields in response: $RESP"
fi

ANALYZED=$(echo "$RESP" | json_get 'patterns_analyzed')
SUBMITTED=$(echo "$RESP" | json_get 'proposals_submitted')
pass "Analyzer ran: patterns_analyzed=$ANALYZED proposals_submitted=$SUBMITTED"

# Si hubo proposals submitted, verificá que aparezcan listadas en Nexus.
if [ "$SUBMITTED" -gt 0 ]; then
  echo "Verifying proposals appear in Nexus /v1/learning/proposals..."
  PROPOSALS=$(api_get "/v1/learning/proposals?org_id=$ORG_ID")
  COUNT=$(echo "$PROPOSALS" | python3 -c "
import json, sys
d = json.load(sys.stdin)
items = d.get('items') or d.get('data') or []
print(len(items))
")
  if [ "$COUNT" -ge "$SUBMITTED" ]; then
    pass "Nexus lists $COUNT proposal(s) (>= $SUBMITTED submitted)"
  else
    fail "Submitted $SUBMITTED but Nexus only lists $COUNT"
  fi
else
  pass "No proposals submitted (insufficient pattern history) — endpoint roundtrip still verified"
fi

# --- (b) /v1/governance-assist/explain/{request_id}: contextualizer ---
# Generamos un request real proponiendo una task (reciclando el flow base) y
# pedimos su explicación. Esto verifica que el contextualizer puede traer el
# request de Nexus y devolver un summary contextualizado.
echo "Setting up a real Nexus request to test the contextualizer..."
CREATE=$(companion_post "/v1/tasks" "{\"title\":\"smoke-ga-explain-$(date +%s)\",\"goal\":\"explain target\",\"created_by\":\"smoke-script\"}")
TASK_ID=$(echo "$CREATE" | json_get 'id')
CONNECTOR_ID=$(ensure_mock_connector)
companion_put "/v1/tasks/$TASK_ID/execution-plan" "{\"connector_id\":\"$CONNECTOR_ID\",\"operation\":\"mock.echo\",\"payload\":{\"message\":\"smoke ga explain\"}}" >/dev/null
PROP=$(companion_post "/v1/tasks/$TASK_ID/propose" '{"note":"smoke ga explain"}')
REQ_ID=$(echo "$PROP" | json_get 'governance_submit.request_id')
[ -n "$REQ_ID" ] && pass "Nexus request created: $REQ_ID" || fail "No governance_submit.request_id"

echo "Calling GET /v1/governance-assist/explain/$REQ_ID..."
EXPLAIN=$(companion_get "/v1/governance-assist/explain/$REQ_ID")
if echo "$EXPLAIN" | python3 -c "
import json, sys
d = json.load(sys.stdin)
ok = d.get('request_id') and isinstance(d.get('summary'), str) and 'degraded' in d
sys.exit(0 if ok else 1)
"; then
  SUMMARY=$(echo "$EXPLAIN" | json_get 'summary')
  DEGRADED=$(echo "$EXPLAIN" | json_get 'degraded')
  pass "Explain returned valid shape (degraded=$DEGRADED, len(summary)=${#SUMMARY})"
else
  fail "Explain response shape invalid: $EXPLAIN"
fi

echo ""
green "=== Companion governance-assist smoke passed ==="
