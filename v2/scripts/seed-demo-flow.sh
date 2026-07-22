#!/usr/bin/env bash
set -Eeuo pipefail
trap 'echo "seed demo failed at line $LINENO" >&2' ERR

BFF_URL="${BFF_URL:-http://127.0.0.1:19080}"
CONSOLE_URL="${CONSOLE_URL:-http://localhost:19173}"
DEV_ACTOR_ID="${DEV_ACTOR_ID:-dev-user}"
DEV_ACTOR_EMAIL="${DEV_ACTOR_EMAIL:-dev@example.local}"
DEV_ORG_ID="${DEV_ORG_ID:-dev-org}"
RUN_ID="$(date +%Y%m%d%H%M%S)"

DEMO_JOB_ROLE_NAME="${DEMO_JOB_ROLE_NAME:-Demo Approval Role}"
DEMO_PROFILE_NAME="${DEMO_PROFILE_NAME:-Demo Approval Profile}"
DEMO_VIRPLOYEE_NAME="${DEMO_VIRPLOYEE_NAME:-Demo Approval Virployee}"
DEMO_TITLE="Demo Approval $RUN_ID"
DEMO_CLEANUP="${DEMO_CLEANUP:-true}"
DEMO_FIXTURE_NAME_RE='^(Demo|Smoke|Manual|Real) Approval (Virployee|Role|Profile)( |$)'
DEMO_APPROVAL_REASON_RE='(Demo|Smoke|Manual Local|Manual|Real) Approval'

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

PRINCIPAL_ID="$(jq -r '.principal_id // .actor_id // empty' <<<"$session")"
ORG_ID="$(jq -r '.organizations[0].id // .org_id // empty' <<<"$session")"
PRODUCT_SURFACE="$(jq -r '.organizations[0].products[0].product_surface // "axis"' <<<"$session")"

if [[ -z "$ORG_ID" || -z "$PRINCIPAL_ID" ]]; then
  echo "could not resolve dev organization/principal from /api/session" >&2
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
      -H "X-Org-ID: $ORG_ID" \
      -H "X-Product-Surface: $PRODUCT_SURFACE" \
      -H "X-Actor-ID: $PRINCIPAL_ID" \
      -d "$body"
  else
    curl -sS --fail-with-body -X "$method" "$BFF_URL$path" \
      -H "X-Org-ID: $ORG_ID" \
      -H "X-Product-Surface: $PRODUCT_SURFACE" \
      -H "X-Actor-ID: $PRINCIPAL_ID"
  fi
}

reject_demo_approvals() {
  local list ids id payload

  list="$(api GET "/api/approvals?status=pending")"
  ids="$(
    jq -r --arg re "$DEMO_APPROVAL_REASON_RE" \
      '.items[]? | select((.reason // "") | test($re)) | .id' \
      <<<"$list"
  )"
  payload="$(jq -n '{note: "demo seed cleanup"}')"

  while IFS= read -r id; do
    [[ -z "$id" ]] && continue
    api POST "/api/approvals/$id/reject" "$payload" >/dev/null || true
  done <<<"$ids"
}

purge_demo_resource() {
  local resource="$1"
  local lifecycle path list ids id

  for lifecycle in active archived trash; do
    case "$lifecycle" in
      active) path="/api/$resource" ;;
      archived) path="/api/$resource?lifecycle=archived" ;;
      trash) path="/api/$resource?lifecycle=trash" ;;
    esac

    list="$(api GET "$path" || true)"
    ids="$(
      jq -r --arg re "$DEMO_FIXTURE_NAME_RE" \
        '.data[]? | select((.name // "") | test($re)) | .id' \
        <<<"$list"
    )"

    while IFS= read -r id; do
      [[ -z "$id" ]] && continue
      if api DELETE "/api/$resource/$id/purge" >/dev/null 2>&1; then
        continue
      fi
      api POST "/api/$resource/$id/trash" >/dev/null 2>&1 || true
      api DELETE "/api/$resource/$id/purge" >/dev/null 2>&1 || true
    done <<<"$ids"
  done
}

cleanup_demo_fixtures() {
  reject_demo_approvals
  purge_demo_resource "virployees"
  purge_demo_resource "job-roles"
  purge_demo_resource "profile-templates"
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
        description: "Demo approval flow action type",
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
        description: "Demo approval flow action type",
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
  local evidence="$7"
  local rollback_key="$8"
  local list id create_payload update_payload manifest rollback_mode

  create_payload="$(
    jq -n \
      --arg key "$key" \
      --arg name "$name" \
      --arg autonomy "$autonomy" \
      --arg risk "$risk" \
      --arg side "$side_effect" \
      --argjson approval "$requires_approval" \
      --argjson evidence "$evidence" \
      --arg rollback "$rollback_key" \
      '{
        capability_key: $key,
        name: $name,
        description: "Demo approval flow capability",
        required_autonomy: $autonomy,
        risk_class: $risk,
        side_effect_class: $side,
        requires_nexus_approval: $approval,
        evidence_required: $evidence,
        rollback_capability_key: $rollback
      }'
  )"

  list="$(api GET "/api/capabilities")"
  id="$(jq -r --arg key "$key" '.data[]? | select(.capability_key == $key) | .id' <<<"$list" | head -n 1)"
  if [[ -n "$id" ]]; then
    # Upsert the governance contract on re-runs (capabilities are not purged).
    update_payload="$(jq 'del(.capability_key)' <<<"$create_payload")"
    api PUT "/api/capabilities/$id" "$update_payload" >/dev/null
  else
    id="$(api POST "/api/capabilities" "$create_payload" | jq -r '.id')"
  fi

  rollback_mode="none"
  if [[ "$side_effect" == "write" ]]; then
    rollback_mode="manual"
  fi
  if [[ -n "$rollback_key" ]]; then
    rollback_mode="automatic"
  fi
  manifest="$(
    jq -n \
      --arg side "$side_effect" \
      --arg scope "$key" \
      --arg rollbackMode "$rollback_mode" \
      '{
        version: "1.0.0",
        product_surface: "axis",
        input_schema: {type: "object"},
        output_schema: {type: "object"},
        required_scopes: [$scope],
        idempotency: (if $side == "write" then {mode: "required", key_fields: ["org_id", "virployee_id", "binding_hash"]} else {mode: "not_applicable", key_fields: []} end),
        rollback_mode: $rollbackMode,
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

ensure_job_role() {
  local list id payload

  list="$(api GET "/api/job-roles")"
  id="$(jq -r --arg name "$DEMO_JOB_ROLE_NAME" '.data[]? | select(.name == $name) | .id' <<<"$list" | head -n 1)"
  if [[ -n "$id" ]]; then
    echo "$id"
    return
  fi

  payload="$(
    jq -n \
      --arg name "$DEMO_JOB_ROLE_NAME" \
      --arg slug "demo-approval-role" \
      '{name: $name, slug: $slug, mission: "Exercise the approval checkpoint flow from the console"}'
  )"
  api POST "/api/job-roles" "$payload" | jq -r '.id'
}

ensure_employer() {
  local employer_list employer_id
  employer_list="$(api GET "/api/work-subjects")"
  employer_id="$(jq -r '.data[]? | select(.external_ref == "demo-approval-employer") | .id' <<<"$employer_list" | head -n 1)"
  if [[ -z "$employer_id" ]]; then
    employer_id="$(api POST "/api/work-subjects" '{"kind":"organization","display_name":"Demo Approval Employer","external_ref":"demo-approval-employer"}' | jq -r '.id')"
  fi
  echo "$employer_id"
}

ensure_profile_template() {
  local list id payload

  list="$(api GET "/api/profile-templates")"
  id="$(jq -r --arg name "$DEMO_PROFILE_NAME" '.data[]? | select(.name == $name) | .id' <<<"$list" | head -n 1)"
  if [[ -n "$id" ]]; then
    echo "$id"
    return
  fi

  payload="$(
    jq -n \
      --arg name "$DEMO_PROFILE_NAME" \
      '{
        name: $name,
        description: "Demo approval flow profile",
        system_prompt: "You are a demo assistant for safe calendar actions.",
        max_autonomy: "A3"
      }'
  )"
  api POST "/api/profile-templates" "$payload" | jq -r '.id'
}

ensure_virployee() {
  local job_role_id="$1"
  local profile_template_id="$2"
  local read_capability_id="$3"
  local create_capability_id="$4"
  local delete_capability_id="$5"
  local employer_id="$6"
  local list id payload

  payload="$(
    jq -n \
      --arg name "$DEMO_VIRPLOYEE_NAME" \
      --arg jobRoleID "$job_role_id" \
      --arg profileTemplateID "$profile_template_id" \
      --arg supervisorUserID "$PRINCIPAL_ID" \
      --arg readCapabilityID "$read_capability_id" \
      --arg createCapabilityID "$create_capability_id" \
      --arg deleteCapabilityID "$delete_capability_id" \
	  --arg employerSubjectID "$employer_id" \
      '{
        name: $name,
        job_role_id: $jobRoleID,
        profile_template_id: $profileTemplateID,
        capability_ids: [$readCapabilityID, $createCapabilityID, $deleteCapabilityID],
        description: "Demo approval flow virployee",
        supervisor_user_id: $supervisorUserID,
		autonomy: "A3",
		employer_subject_id: $employerSubjectID
      }'
  )"

  list="$(api GET "/api/virployees")"
  id="$(jq -r --arg name "$DEMO_VIRPLOYEE_NAME" '.data[]? | select(.name == $name) | .id' <<<"$list" | head -n 1)"
  if [[ -n "$id" ]]; then
    api PUT "/api/virployees/$id" "$payload" >/dev/null
    echo "$id"
    return
  fi

  api POST "/api/virployees" "$payload" | jq -r '.id'
}

run_dry_run() {
  local virployee_id="$1"
  local input="$2"
  local payload

  payload="$(jq -n --arg input "$input" '{input: $input}')"
  api POST "/api/virployees/$virployee_id/dry-run" "$payload"
}

configure_scope_policy() {
  local virployee_id="$1"
  local current revision payload
  current="$(api GET "/api/virployees/$virployee_id/scope-policy")"
  revision="$(jq -r '.revision // 0' <<<"$current")"
  payload="$(jq -n --argjson revision "$revision" '{allowed_topics:["calendar"],prohibited_topics:[],out_of_scope:"abstain",expected_revision:$revision}')"
  api PUT "/api/virployees/$virployee_id/scope-policy" "$payload" >/dev/null
}

run_gate() {
  local virployee_id="$1"
  local input="$2"
  local confirmed_calendar_create="${3:-false}"
  local payload

  if [[ "$confirmed_calendar_create" == "true" ]]; then
    payload="$(
      jq -n \
        --arg input "$input" \
        --arg title "$DEMO_TITLE" \
        '{
          input: $input,
          confirmed_draft: {
            action: "calendar.events.create",
            kind: "calendar_event",
            fields: [
              {key: "title", value: $title},
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

latest_runs() {
  local virployee_id="$1"
  api GET "/api/virployees/$virployee_id/runs?limit=20"
}

if [[ "$DEMO_CLEANUP" == "true" ]]; then
  cleanup_demo_fixtures
fi

read_action_id="$(ensure_action_type "calendar.events.read" "Read calendar events" "low" "true")"
create_action_id="$(ensure_action_type "calendar.events.create" "Create calendar events" "high" "true")"
delete_action_id="$(ensure_action_type "calendar.events.delete" "Delete calendar events" "high" "true")"

# calendar.events.delete is the compensation (rollback) for create, so it is
# created first and referenced by the create capability's rollback_capability_key.
# create/delete are write actions that require human approval (F6, G3.4/G3.5).
ensure_quota_policy "inbound"
ensure_quota_policy "llm"
ensure_quota_policy "executors"

read_capability_id="$(ensure_capability "calendar.events.read" "Read calendar events" "A1" "low" "read" "false" "true" "")"
delete_capability_id="$(ensure_capability "calendar.events.delete" "Delete calendar events" "A3" "high" "write" "true" "true" "")"
create_capability_id="$(ensure_capability "calendar.events.create" "Create calendar events" "A2" "high" "write" "true" "true" "calendar.events.delete")"
job_role_id="$(ensure_job_role)"
profile_template_id="$(ensure_profile_template)"
employer_id="$(ensure_employer)"
virployee_id="$(ensure_virployee "$job_role_id" "$profile_template_id" "$read_capability_id" "$create_capability_id" "$delete_capability_id" "$employer_id")"
configure_scope_policy "$virployee_id"

read_input="Que reuniones tengo manana"
create_input="Agenda una reunion \"$DEMO_TITLE\" manana a las 15 con ana@example.com"

dry_run="$(run_dry_run "$virployee_id" "$read_input")"
assert_jq "$dry_run" '.decision == "allowed"' "read dry-run should be allowed"

allow_gate="$(run_gate "$virployee_id" "$read_input")"
assert_jq "$allow_gate" '.execution_gate.decision == "pass"' "read gate should pass"

require_gate="$(run_gate "$virployee_id" "$create_input" true)"
assert_jq "$require_gate" '.execution_gate.decision == "blocked"' "create gate should require approval"

runs="$(latest_runs "$virployee_id")"
assert_jq "$runs" '[.data[]? | select((.input_preview // "") | contains($title)) | select(.nexus_result.decision == "require_approval" and (.nexus_result.approval_id // "") != "")] | length >= 1' "run history should include this approval trace" --arg title "$DEMO_TITLE"

approval_id="$(
  jq -r --arg title "$DEMO_TITLE" \
    '[.data[]? | select((.input_preview // "") | contains($title)) | select(.nexus_result.decision == "require_approval" and (.nexus_result.approval_id // "") != "")][0].nexus_result.approval_id' \
    <<<"$runs"
)"

pending_approval="$(api GET "/api/approvals/$approval_id")"
assert_jq "$pending_approval" '.status == "pending"' "demo approval should remain pending"

cat <<EOF
demo seed ready
org_id=$ORG_ID
org_id=$ORG_ID
principal_id=$PRINCIPAL_ID
read_action_id=$read_action_id
create_action_id=$create_action_id
delete_action_id=$delete_action_id
read_capability_id=$read_capability_id
create_capability_id=$create_capability_id
delete_capability_id=$delete_capability_id
virployee_name=$DEMO_VIRPLOYEE_NAME
virployee_id=$virployee_id
approval_id=$approval_id
console_url=$CONSOLE_URL

manual check:
1. Open $CONSOLE_URL
2. Go to Virployees and search "$DEMO_VIRPLOYEE_NAME"
3. Select it, open Dry Run, and review Run history
4. Go to Approvals and review approval $approval_id
5. Approve or reject it, then return to the Virployee history
6. If approved, click "Execute locally" and confirm the local event and Nexus report were recorded
EOF
