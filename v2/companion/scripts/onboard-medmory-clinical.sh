#!/usr/bin/env bash
set -euo pipefail

: "${AXIS_COMPANION_URL:?set AXIS_COMPANION_URL, including /v1}"
: "${AXIS_INTERNAL_AUTH_SECRET:?set AXIS_INTERNAL_AUTH_SECRET}"
: "${AXIS_TENANT_ID:?set AXIS_TENANT_ID}"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
companion_dir="$(cd "${script_dir}/.." && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf -- "${tmp_dir}"' EXIT

api() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  local args=(
    -fsS -X "${method}"
    -H "X-Axis-Internal-Token: ${AXIS_INTERNAL_AUTH_SECRET}"
    -H "X-Tenant-ID: ${AXIS_TENANT_ID}"
    -H "X-Actor-ID: onboarding:medmory-clinical"
    -H "X-Axis-Tenant-Role: admin"
    -H "Content-Type: application/json"
  )
  if [[ -n "${body}" ]]; then
    args+=(-d "${body}")
  fi
  curl "${args[@]}" "${AXIS_COMPANION_URL%/}${path}"
}

for area in inbound embeddings llm; do
  api PUT "/quota-policies/medmory/${area}" \
    '{"window_seconds":3600,"request_limit":10000,"unit_limit":10000000,"active":true}' >/dev/null
done

(cd "${companion_dir}" && go run ./cmd/clinical-capability-manifests) >"${tmp_dir}/definitions.json"
api GET /capabilities >"${tmp_dir}/capabilities.json"

while IFS= read -r definition; do
  key="$(jq -r '.capability_key' <<<"${definition}")"
  capability_id="$(jq -r --arg key "${key}" '.data[] | select(.capability_key==$key) | .id' "${tmp_dir}/capabilities.json" | head -n1)"
  if [[ -z "${capability_id}" ]]; then
    create_body="$(jq -c 'del(.manifest,.job_role_names)' <<<"${definition}")"
    created="$(api POST /capabilities "${create_body}")"
    capability_id="$(jq -r '.id' <<<"${created}")"
  fi
  manifest_body="$(jq -c '.manifest' <<<"${definition}")"
  api PUT "/capabilities/${capability_id}/manifest" "${manifest_body}" >/dev/null
  api POST "/capabilities/${capability_id}/conform" '{}' >/dev/null
  api POST "/capabilities/${capability_id}/activate" '{}' >/dev/null
  jq -n --arg key "${key}" --arg id "${capability_id}" '{key:$key,id:$id}' >>"${tmp_dir}/ids.jsonl"
done < <(jq -c '.[]' "${tmp_dir}/definitions.json")

policy="$(api GET /runtime/mcp-policy)"
policy_body="$(jq -c '
  .enabled=true |
  .kill_switch=false |
  .max_risk_class=(if (.max_risk_class=="low") then "medium" else .max_risk_class end) |
  .allowed_capabilities=((.allowed_capabilities + ["clinical.records.search","clinical.timeline.build"]) | unique) |
  .product_rules=(.product_rules + {medmory:{disabled:false,allowed_capabilities:["clinical.records.search","clinical.timeline.build"],denied_capabilities:[]}}) |
  {enabled,kill_switch,allowed_capabilities,denied_capabilities,capability_kill_switches,max_risk_class,
   max_calls_per_minute,max_concurrency,product_rules,job_role_rules,expected_version:.version}
' <<<"${policy}")"
api PUT /runtime/mcp-policy "${policy_body}" >/dev/null

api GET /job-roles >"${tmp_dir}/job_roles.json"
api GET /virployees >"${tmp_dir}/virployees.json"

while IFS= read -r virployee; do
  virployee_id="$(jq -r '.id' <<<"${virployee}")"
  job_role_id="$(jq -r '.job_role_id' <<<"${virployee}")"
  role_name="$(jq -r --arg id "${job_role_id}" '.data[] | select(.id==$id) | .name' "${tmp_dir}/job_roles.json")"
  capability_keys=()
  case "${role_name}" in
    "Medical Historian") capability_keys=(clinical.records.search clinical.timeline.build) ;;
    "Study Analyst"|"Care Coordinator") capability_keys=(clinical.timeline.build) ;;
    *) continue ;;
  esac
  capability_ids="$(jq -n '[]')"
  for key in "${capability_keys[@]}"; do
    id="$(jq -r --arg key "${key}" 'select(.key==$key) | .id' "${tmp_dir}/ids.jsonl")"
    capability_ids="$(jq -c --arg id "${id}" '. + [$id] | unique' <<<"${capability_ids}")"
  done
  update_body="$(jq -c --argjson clinical "${capability_ids}" '
    {name,job_role_id,profile_template_id,
     capability_ids:((.capability_ids + $clinical)|unique),description,supervisor_user_id,autonomy,grounding_mode}
  ' <<<"${virployee}")"
  api PUT "/virployees/${virployee_id}" "${update_body}" >/dev/null
done < <(jq -c '.data[]' "${tmp_dir}/virployees.json")

echo "Medmory clinical capabilities are conformant, active, policy-enabled, and assigned for tenant ${AXIS_TENANT_ID}."
