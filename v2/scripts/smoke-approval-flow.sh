#!/usr/bin/env bash
set -Eeuo pipefail
trap 'echo "approval flow failed at line $LINENO" >&2' ERR

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
    curl -sS --fail-with-body -X "$method" "$BFF_URL$path" \
      -H "Content-Type: application/json" \
      -H "X-Tenant-ID: $TENANT_ID" \
      -H "X-Actor-ID: $PRINCIPAL_ID" \
      -d "$body"
  else
    curl -sS --fail-with-body -X "$method" "$BFF_URL$path" \
      -H "X-Tenant-ID: $TENANT_ID" \
      -H "X-Actor-ID: $PRINCIPAL_ID"
  fi
}

assert_jq() {
  local json="$1"
  local expr="$2"
  local message="$3"
  shift 3
  if ! jq -e "$@" "$expr" >/dev/null <<<"$json"; then
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
  local risk="$4"
  local side_effect="$5"
  local requires_approval="$6"
  local list id payload manifest

  list="$(api GET "/api/capabilities")"
  id="$(jq -r --arg key "$key" '.data[]? | select(.capability_key == $key) | .id' <<<"$list" | head -n 1)"
  payload="$(
    jq -n \
      --arg key "$key" \
      --arg name "$name" \
      --arg autonomy "$autonomy" \
      --arg risk "$risk" \
      --arg side "$side_effect" \
      --argjson approval "$requires_approval" \
      '{
        capability_key: $key,
        name: $name,
        description: "Smoke approval flow capability",
        required_autonomy: $autonomy,
        risk_class: $risk,
        side_effect_class: $side,
        requires_nexus_approval: $approval,
        evidence_required: true
      }'
  )"
  if [[ -n "$id" ]]; then
    api PUT "/api/capabilities/$id" "$(jq 'del(.capability_key)' <<<"$payload")" >/dev/null
  else
    id="$(api POST "/api/capabilities" "$payload" | jq -r '.id')"
  fi

  manifest="$(
    jq -n \
      --arg side "$side_effect" \
      --arg scope "$key" \
      '{
        version: "1.0.0",
        product_surface: "axis",
        input_schema: {type: "object"},
        output_schema: {type: "object"},
        required_scopes: [$scope],
        idempotency: (if $side == "write" then {mode: "required", key_fields: ["tenant_id", "virployee_id", "binding_hash"]} else {mode: "not_applicable", key_fields: []} end),
        rollback_mode: (if $side == "write" then "manual" else "none" end),
        timeout_ms: 30000,
        retry: {max_attempts: 3, backoff_ms: 1000},
        postconditions: ["Nexus report and evidence are persisted"],
        quota_areas: (if $side == "write" then ["inbound", "llm", "executors"] else ["inbound", "llm"] end),
        secret_refs: [],
        attestation_required: ($side == "write"),
        cost_class: "low"
      }'
  )"
  api PUT "/api/capabilities/$id/manifest" "$manifest" >/dev/null
  api POST "/api/capabilities/$id/conform" >/dev/null
  api POST "/api/capabilities/$id/activate" >/dev/null
  echo "$id"
}

ensure_quota_policy() {
  local area="$1"
  api PUT "/api/quota-policies/axis/$area" '{"window_seconds":60,"request_limit":1000,"unit_limit":100000000,"active":true}' >/dev/null
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

create_employer() {
  api POST "/api/work-subjects" "$(jq -n --arg ref "smoke-approval-$RUN_ID" '{kind:"organization",display_name:"Smoke Approval Employer",external_ref:$ref}')" | jq -r '.id'
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
  local employer_id="$5"
  local payload

  payload="$(
    jq -n \
      --arg name "Smoke Approval Virployee $RUN_ID" \
      --arg jobRoleID "$job_role_id" \
      --arg profileTemplateID "$profile_template_id" \
      --arg supervisorUserID "$PRINCIPAL_ID" \
      --arg readCapabilityID "$read_capability_id" \
      --arg createCapabilityID "$create_capability_id" \
	  --arg employerSubjectID "$employer_id" \
      '{
        name: $name,
        job_role_id: $jobRoleID,
        profile_template_id: $profileTemplateID,
        capability_ids: [$readCapabilityID, $createCapabilityID],
        description: "Smoke approval flow virployee",
        supervisor_user_id: $supervisorUserID,
		autonomy: "A3",
		employer_subject_id: $employerSubjectID
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
            {key: "date", value: "2099-01-01"},
            {key: "time", value: "15:00"},
            {key: "timezone", value: "America/Argentina/Buenos_Aires"},
            {key: "duration_minutes", value: "60"},
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

configure_scope_policy() {
  local virployee_id="$1"
  api PUT "/api/virployees/$virployee_id/scope-policy" '{"allowed_topics":["calendar"],"prohibited_topics":[],"out_of_scope":"abstain","expected_revision":0}' >/dev/null
}

latest_runs() {
  local virployee_id="$1"
  api GET "/api/virployees/$virployee_id/runs?limit=20"
}

echo "tenant: $TENANT_ID"

ensure_quota_policy "inbound"
ensure_quota_policy "llm"
ensure_quota_policy "executors"

read_action_id="$(ensure_action_type "calendar.events.read" "Read calendar events" "low" "true")"
create_action_id="$(ensure_action_type "calendar.events.create" "Create calendar events" "high" "true")"

read_capability_id="$(ensure_capability "calendar.events.read" "Read calendar events" "A1" "low" "read" "false")"
create_capability_id="$(ensure_capability "calendar.events.create" "Create calendar events" "A2" "high" "write" "true")"
job_role_id="$(create_job_role)"
profile_template_id="$(create_profile_template)"
employer_id="$(create_employer)"
virployee_id="$(create_virployee "$job_role_id" "$profile_template_id" "$read_capability_id" "$create_capability_id" "$employer_id")"
configure_scope_policy "$virployee_id"

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

execution_payload="$(jq -n --arg approvalID "$approval_id" '{approval_id: $approvalID}')"
simulated_execution="$(api POST "/api/virployees/$virployee_id/executions" "$execution_payload")"
assert_jq "$simulated_execution" '.operation == "execution"' "approved approval should create an execution trace"
assert_jq "$simulated_execution" '.execution_result.status == "succeeded" and .execution_result.mode == "local" and (.execution_result.nexus_report_status == "pending" or .execution_result.nexus_report_status == "reported")' "local execution should succeed and enqueue its Nexus report"

# Nexus delivery is an outbox projection now. Re-entering the execution endpoint
# is idempotent and returns the latest projection without repeating the effect.
for _ in $(seq 1 20); do
  if jq -e '.execution_result.nexus_report_status == "reported"' >/dev/null <<<"$simulated_execution"; then
    break
  fi
  sleep 0.25
  simulated_execution="$(api POST "/api/virployees/$virployee_id/executions" "$execution_payload")"
done
assert_jq "$simulated_execution" '.execution_result.nexus_report_status == "reported"' "outbox should eventually report the execution to Nexus"

simulated_trace_id="$(jq -r '.id' <<<"$simulated_execution")"
simulated_replay="$(api POST "/api/virployees/$virployee_id/executions" "$execution_payload")"
assert_jq "$simulated_replay" '.id == $traceID' "simulated execution should be idempotent for the same approval" --arg traceID "$simulated_trace_id"

ensure_action_type "calendar.events.create" "Create calendar events" "high" "false" >/dev/null
deny_gate="$(run_gate "$virployee_id" "Agenda una reunion \"Smoke Deny\" manana a las 16 con ana@example.com" true)"
assert_jq "$deny_gate" '.execution_gate.decision == "blocked"' "disabled action type should block"

runs="$(latest_runs "$virployee_id")"
assert_jq "$runs" '[.data[]? | select(.operation == "execution" and .execution_result.status == "succeeded")] | length >= 1' "run history should include execution trace"
assert_jq "$runs" '[.data[]? | select(.nexus_result.decision == "deny" and .nexus_result.status == "denied")] | length >= 1' "run history should include deny trace"

ensure_action_type "calendar.events.create" "Create calendar events" "high" "true" >/dev/null

echo "approval flow smoke passed"
echo "read_action_id=$read_action_id"
echo "create_action_id=$create_action_id"
echo "virployee_id=$virployee_id"
echo "approval_id=$approval_id"
