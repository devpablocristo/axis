#!/usr/bin/env bash
set -euo pipefail

BFF_URL="${BFF_URL:-http://127.0.0.1:19080}"
DEV_ACTOR_ID="${DEV_ACTOR_ID:-dev-user}"
DEV_ACTOR_EMAIL="${DEV_ACTOR_EMAIL:-dev@example.local}"
DEV_ORG_ID="${DEV_ORG_ID:-dev-org}"
RUN_ID="$(date +%Y%m%d%H%M%S)"

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

need curl
need jq

session="$(
  curl -fsS "$BFF_URL/api/session" \
    -H "X-Actor-ID: $DEV_ACTOR_ID" \
    -H "X-Actor-Email: $DEV_ACTOR_EMAIL" \
    -H "X-Axis-Org-ID: $DEV_ORG_ID"
)"

TENANT_ID="$(jq -r '.org_id as $orgID | ([.tenants[]? | select(.org_id == $orgID)][0].id // .tenants[0].id // empty)' <<<"$session")"
PRINCIPAL_ID="$(jq -r '.principal_id // .actor_id // empty' <<<"$session")"

if [[ -z "$TENANT_ID" || -z "$PRINCIPAL_ID" ]]; then
  echo "could not resolve dev tenant/principal from /api/session" >&2
  echo "$session" >&2
  exit 1
fi

api() {
  local method="$1"
  local path="$2"
  local body="${3:-}"

  if [[ -n "$body" ]]; then
    curl -fsS -X "$method" "$BFF_URL$path" \
      -H "Content-Type: application/json" \
      -H "X-Tenant-ID: $TENANT_ID" \
      -H "X-Actor-ID: $PRINCIPAL_ID" \
      -d "$body"
  else
    curl -fsS -X "$method" "$BFF_URL$path" \
      -H "X-Tenant-ID: $TENANT_ID" \
      -H "X-Actor-ID: $PRINCIPAL_ID"
  fi
}

assert_jq() {
  local json="$1"
  local expr="$2"
  local message="$3"
  if ! jq -e "$expr" >/dev/null <<<"$json"; then
    echo "assertion failed: $message" >&2
    echo "$json" | jq . >&2
    exit 1
  fi
}

ensure_action_type() {
  local key="$1"
  local name="$2"
  local risk="$3"
  local enabled="$4"
  local list id create_payload update_payload

  list="$(api GET "/api/action-types")"
  id="$(jq -r --arg key "$key" '.data[]? | select(.action_type_key == $key) | .id' <<<"$list" | head -n 1)"
  create_payload="$(
    jq -n \
      --arg key "$key" \
      --arg name "$name" \
      --arg risk "$risk" \
      --argjson enabled "$enabled" \
      '{
        action_type_key: $key,
        name: $name,
        description: "Smoke approval flow action type",
        category: "calendar",
        risk_class: $risk,
        enabled: $enabled
      }'
  )"
  update_payload="$(
    jq -n \
      --arg name "$name" \
      --arg risk "$risk" \
      --argjson enabled "$enabled" \
      '{
        name: $name,
        description: "Smoke approval flow action type",
        category: "calendar",
        risk_class: $risk,
        enabled: $enabled
      }'
  )"

  if [[ -n "$id" ]]; then
    api PUT "/api/action-types/$id" "$update_payload" >/dev/null
    echo "$id"
  else
    api POST "/api/action-types" "$create_payload" | jq -r '.id'
  fi
}

ensure_capability() {
  local key="$1"
  local name="$2"
  local autonomy="$3"
  local list id payload

  list="$(api GET "/api/capabilities")"
  id="$(jq -r --arg key "$key" '.data[]? | select(.capability_key == $key) | .id' <<<"$list" | head -n 1)"
  if [[ -n "$id" ]]; then
    echo "$id"
    return
  fi

  payload="$(
    jq -n \
      --arg key "$key" \
      --arg name "$name" \
      --arg autonomy "$autonomy" \
      '{
        capability_key: $key,
        name: $name,
        description: "Smoke approval flow capability",
        required_autonomy: $autonomy
      }'
  )"
  api POST "/api/capabilities" "$payload" | jq -r '.id'
}

create_job_role() {
  local payload
  payload="$(
    jq -n \
      --arg name "Smoke Approval Role $RUN_ID" \
      --arg slug "smoke-approval-$RUN_ID" \
      '{name: $name, slug: $slug, mission: "Exercise the approval checkpoint flow"}'
  )"
  api POST "/api/job-roles" "$payload" | jq -r '.id'
}

create_profile_template() {
  local payload
  payload="$(
    jq -n \
      --arg name "Smoke Approval Profile $RUN_ID" \
      '{
        name: $name,
        description: "Smoke approval flow profile",
        system_prompt: "You are a smoke-test assistant for calendar actions.",
        max_autonomy: "A3"
      }'
  )"
  api POST "/api/profile-templates" "$payload" | jq -r '.id'
}

create_virployee() {
  local job_role_id="$1"
  local profile_template_id="$2"
  local read_capability_id="$3"
  local create_capability_id="$4"
  local payload

  payload="$(
    jq -n \
      --arg name "Smoke Approval Virployee $RUN_ID" \
      --arg jobRoleID "$job_role_id" \
      --arg profileTemplateID "$profile_template_id" \
      --arg supervisorUserID "$PRINCIPAL_ID" \
      --arg readCapabilityID "$read_capability_id" \
      --arg createCapabilityID "$create_capability_id" \
      '{
        name: $name,
        job_role_id: $jobRoleID,
        profile_template_id: $profileTemplateID,
        capability_ids: [$readCapabilityID, $createCapabilityID],
        description: "Smoke approval flow virployee",
        supervisor_user_id: $supervisorUserID,
        autonomy: "A3"
      }'
  )"
  api POST "/api/virployees" "$payload" | jq -r '.id'
}

run_gate() {
  local virployee_id="$1"
  local input="$2"
  local confirmed_calendar_create="${3:-false}"
  local payload

  if [[ "$confirmed_calendar_create" == "true" ]]; then
    payload="$(
      jq -n --arg input "$input" '{
        input: $input,
        confirmed_draft: {
          action: "calendar.events.create",
          kind: "calendar_event",
          fields: [
            {key: "title", value: "Smoke Approval"},
            {key: "date_hint", value: "manana"},
            {key: "time", value: "15:00"},
            {key: "attendees", value: "ana@example.com"}
          ]
        }
      }'
    )"
  else
    payload="$(jq -n --arg input "$input" '{input: $input}')"
  fi
  api POST "/api/virployees/$virployee_id/execution-gate" "$payload"
}

latest_runs() {
  local virployee_id="$1"
  api GET "/api/virployees/$virployee_id/runs?limit=20"
}

echo "tenant: $TENANT_ID"

read_action_id="$(ensure_action_type "calendar.events.read" "Read calendar events" "low" "true")"
create_action_id="$(ensure_action_type "calendar.events.create" "Create calendar events" "high" "true")"

read_capability_id="$(ensure_capability "calendar.events.read" "Read calendar events" "A1")"
create_capability_id="$(ensure_capability "calendar.events.create" "Create calendar events" "A2")"
job_role_id="$(create_job_role)"
profile_template_id="$(create_profile_template)"
virployee_id="$(create_virployee "$job_role_id" "$profile_template_id" "$read_capability_id" "$create_capability_id")"

allow_gate="$(run_gate "$virployee_id" "Que reuniones tengo manana")"
assert_jq "$allow_gate" '.execution_gate.decision == "pass"' "calendar read should pass"

require_gate="$(run_gate "$virployee_id" "Agenda una reunion \"Smoke Approval\" manana a las 15 con ana@example.com" true)"
assert_jq "$require_gate" '.execution_gate.decision == "blocked"' "high-risk calendar create should block for approval"

runs="$(latest_runs "$virployee_id")"
assert_jq "$runs" '[.data[]? | select(.nexus_result.decision == "allow")] | length >= 1' "run history should include allow trace"
assert_jq "$runs" '[.data[]? | select(.nexus_result.decision == "require_approval" and (.nexus_result.approval_id // "") != "")] | length >= 1' "run history should include approval trace"

approval_id="$(
  jq -r '[.data[]? | select(.nexus_result.decision == "require_approval" and (.nexus_result.approval_id // "") != "")][0].nexus_result.approval_id' <<<"$runs"
)"

pending_approval="$(api GET "/api/approvals/$approval_id")"
assert_jq "$pending_approval" '.status == "pending"' "approval should start pending"

approved_approval="$(api POST "/api/approvals/$approval_id/approve" '{"note":"smoke approved"}')"
assert_jq "$approved_approval" '.status == "approved"' "approval should be approved"

approved_lookup="$(api GET "/api/approvals/$approval_id")"
assert_jq "$approved_lookup" '.status == "approved" and .decided_by != ""' "approved approval should be readable"

ensure_action_type "calendar.events.create" "Create calendar events" "high" "false" >/dev/null
deny_gate="$(run_gate "$virployee_id" "Agenda una reunion \"Smoke Deny\" manana a las 16 con ana@example.com" true)"
assert_jq "$deny_gate" '.execution_gate.decision == "blocked"' "disabled action type should block"

runs="$(latest_runs "$virployee_id")"
assert_jq "$runs" '[.data[]? | select(.nexus_result.decision == "deny" and .nexus_result.status == "denied")] | length >= 1' "run history should include deny trace"

ensure_action_type "calendar.events.create" "Create calendar events" "high" "true" >/dev/null

echo "approval flow smoke passed"
echo "read_action_id=$read_action_id"
echo "create_action_id=$create_action_id"
echo "virployee_id=$virployee_id"
echo "approval_id=$approval_id"
