#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

report_paths=()

run_check() {
  local product="$1"
  local contract="scripts/onboarding/${product}-product-contract.json"
  local eval_pack="scripts/evals/${product}-golden.json"
  local report="$tmpdir/${product}-report.json"

  go run ./cmd/product-onboarding-check \
    -contract "$contract" \
    -eval-pack "$eval_pack" > "$report"
  report_paths+=("$report")
}

run_check reference
run_check shadow

if [[ -n "${AXIS_REAL_PRODUCTS:-}" ]]; then
  normalized_real_products="${AXIS_REAL_PRODUCTS//,/ }"
  for product in $normalized_real_products; do
    product="$(echo "$product" | xargs)"
    if [[ -n "$product" ]]; then
      run_check "$product"
    fi
  done
fi

python3 - "${report_paths[@]}" <<'PY'
import json
import os
import pathlib
import sys

report_paths = [pathlib.Path(p) for p in sys.argv[1:]]
products = {}
orgs = {}
fixture_report_names = {"reference-report.json", "shadow-report.json"}
real_products = {p.strip() for p in os.environ.get("AXIS_REAL_PRODUCTS", "").replace(",", " ").split() if p.strip()}

for path in report_paths:
    report = json.loads(path.read_text())
    product = report.get("product_surface")
    org_id = report.get("org_id")
    if report.get("status") != "passed":
        raise SystemExit(f"{path.name} did not pass: {json.dumps(report, indent=2)}")
    if not product or not org_id:
        raise SystemExit(f"{path.name} missing product_surface/org_id")
    if path.name in fixture_report_names and product in {"ponti", "pymes", "medmory"}:
        raise SystemExit(f"{path.name} uses real product fixture {product!r}")
    if product in products:
        raise SystemExit(f"duplicate product_surface {product!r}")
    if org_id in orgs:
        raise SystemExit(f"duplicate org_id {org_id!r}")
    products[product] = path.name
    orgs[org_id] = path.name

required = {"reference", "shadow"}
missing = required - set(products)
if missing:
    raise SystemExit(f"missing required fixture products {sorted(missing)}")
missing_real = real_products - set(products)
if missing_real:
    raise SystemExit(f"missing requested real products {sorted(missing_real)}")

print(json.dumps({
    "status": "passed",
    "products": sorted(products),
    "orgs": sorted(orgs),
    "checks": [
        "product_contracts",
        "eval_packs",
        "distinct_product_surfaces",
        "distinct_org_ids",
        "no_real_product_defaults",
        "requested_real_products"
    ]
}, indent=2))
PY
