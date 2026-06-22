#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/dev/sync-axis-iam-baseline.sh [--dry-run|--apply]

Syncs the local/dev Axis IAM baseline:
  Clerk Organizations: Medmory, Ponti, Pymes
  Axis Control DB products: medmory, ponti, pymes

Default mode is --dry-run.
EOF
}

mode="dry-run"
case "${1:-}" in
  ""|--dry-run) mode="dry-run" ;;
  --apply) mode="apply" ;;
  -h|--help) usage; exit 0 ;;
  *) usage >&2; exit 2 ;;
esac

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a
fi

require_bin() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

require_bin clerk
require_bin jq
require_bin psql

clerk_secret="${AXIS_CLERK_SECRET_KEY:-${CLERK_SECRET_KEY:-}}"
if [[ -z "$clerk_secret" ]]; then
  echo "AXIS_CLERK_SECRET_KEY or CLERK_SECRET_KEY is required for Clerk API access" >&2
  exit 1
fi

db_url="${AXIS_CONTROL_DATABASE_URL:-postgres://postgres:postgres@localhost:${AXIS_CONTROL_POSTGRES_PORT:-15436}/axis_control?sslmode=disable}"

declare -A baseline_products=(
  ["Medmory"]="medmory"
  ["Ponti"]="ponti"
  ["Pymes"]="pymes"
)
baseline_names=("Medmory" "Ponti" "Pymes")
legacy_names=("owner" "Acme")

clerk_api() {
  clerk api "$@" --secret-key "$clerk_secret"
}

json_orgs_file="$(mktemp)"
trap 'rm -f "$json_orgs_file" "${sql:-}"' EXIT

refresh_orgs() {
  clerk_api '/organizations?limit=100' >"$json_orgs_file"
}

orgs_json() {
  cat "$json_orgs_file"
}

refresh_orgs

org_id_by_name() {
  local name="$1"
  jq -r --arg name "$name" '.data[]? | select(.name == $name) | .id' "$json_orgs_file" | head -n 1
}

print_plan() {
  echo "Mode: $mode"
  echo
  echo "Clerk baseline:"
  for name in "${baseline_names[@]}"; do
    local id
    id="$(org_id_by_name "$name")"
    if [[ -n "$id" ]]; then
      echo "  keep  $name ($id)"
    else
      echo "  create $name"
    fi
  done
  echo
  echo "Clerk legacy cleanup:"
  for name in "${legacy_names[@]}"; do
    local id
    id="$(org_id_by_name "$name")"
    if [[ -n "$id" ]]; then
      echo "  delete $name ($id)"
    else
      echo "  absent $name"
    fi
  done
  echo
  echo "Axis Control DB:"
  for name in "${baseline_names[@]}"; do
    echo "  upsert org $name and product ${baseline_products[$name]}"
  done
  echo "  remove non-baseline axis_products"
}

print_plan

if [[ "$mode" != "apply" ]]; then
  echo
  echo "Dry-run only. Re-run with --apply to mutate Clerk and Axis Control DB."
  exit 0
fi

for name in "${baseline_names[@]}"; do
  if [[ -n "$(org_id_by_name "$name")" ]]; then
    continue
  fi
  payload="$(jq -nc --arg name "$name" \
    '{name: $name, public_metadata: {tenant_type: "axis_org", managed_by: "axis"}}')"
  echo "Creating Clerk organization $name"
  clerk_api /organizations -d "$payload" --yes >/dev/null
done

refresh_orgs

for name in "${legacy_names[@]}"; do
  id="$(org_id_by_name "$name")"
  if [[ -z "$id" ]]; then
    continue
  fi
  echo "Deleting legacy Clerk organization $name ($id)"
  clerk_api "/organizations/$id" -X DELETE --yes >/dev/null
done

refresh_orgs

sql="$(mktemp)"

cat >"$sql" <<'SQL'
CREATE TABLE IF NOT EXISTS axis_orgs (
  id text PRIMARY KEY,
  external_id text UNIQUE,
  provider text NOT NULL DEFAULT '',
  provider_org_id text UNIQUE,
  name text NOT NULL,
  slug text NOT NULL UNIQUE,
  status text NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  synced_at timestamptz
);
CREATE TABLE IF NOT EXISTS axis_products (
  id text PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES axis_orgs(id) ON DELETE CASCADE,
  product_surface text NOT NULL,
  name text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  config_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, product_surface)
);
ALTER TABLE axis_orgs ADD COLUMN IF NOT EXISTS synced_at timestamptz;
SQL

baseline_id_literals=()
baseline_surface_literals=()

for name in "${baseline_names[@]}"; do
  id="$(org_id_by_name "$name")"
  if [[ -z "$id" ]]; then
    echo "failed to resolve Clerk organization $name after create" >&2
    exit 1
  fi
  slug="$(jq -r --arg id "$id" '.data[] | select(.id == $id) | .slug // ""' "$json_orgs_file")"
  surface="${baseline_products[$name]}"
  product_id="product_${surface}"
  baseline_id_literals+=("'${id//\'/\'\'}'")
  baseline_surface_literals+=("'${surface//\'/\'\'}'")
  cat >>"$sql" <<SQL
INSERT INTO axis_orgs (id, external_id, provider, provider_org_id, name, slug, status, created_at, updated_at, synced_at)
VALUES ('$id', '$id', 'clerk', '$id', '$name', '${slug:-$surface}', 'active', now(), now(), now())
ON CONFLICT (id) DO UPDATE SET
  external_id = EXCLUDED.external_id,
  provider = EXCLUDED.provider,
  provider_org_id = EXCLUDED.provider_org_id,
  name = EXCLUDED.name,
  slug = EXCLUDED.slug,
  status = EXCLUDED.status,
  updated_at = now(),
  synced_at = now();

INSERT INTO axis_products (id, tenant_id, product_surface, name, status, config_json, created_at, updated_at)
VALUES ('$product_id', '$id', '$surface', '$name', 'active', '{}'::jsonb, now(), now())
ON CONFLICT (tenant_id, product_surface) DO UPDATE SET
  name = EXCLUDED.name,
  status = EXCLUDED.status,
  updated_at = now();
SQL
done

baseline_ids_csv="$(IFS=,; echo "${baseline_id_literals[*]}")"
baseline_surfaces_csv="$(IFS=,; echo "${baseline_surface_literals[*]}")"

cat >>"$sql" <<SQL
DELETE FROM axis_products
WHERE tenant_id NOT IN ($baseline_ids_csv)
   OR product_surface NOT IN ($baseline_surfaces_csv);

DELETE FROM axis_orgs
WHERE id NOT IN ($baseline_ids_csv);
SQL

echo "Syncing Axis Control DB"
psql "$db_url" --set=ON_ERROR_STOP=1 --file "$sql" >/dev/null

echo "Done."
