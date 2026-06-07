#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

run_check() {
  local product="$1"
  local contract="scripts/onboarding/${product}-product-contract.json"
  local eval_pack="scripts/evals/${product}-golden.json"
  local report="$tmpdir/${product}-report.json"

  go run ./cmd/product-onboarding-check \
    -contract "$contract" \
    -eval-pack "$eval_pack" > "$report"
}

run_check reference
run_check shadow

python3 - "$tmpdir/reference-report.json" "$tmpdir/shadow-report.json" <<'PY'
import json
import pathlib
import sys

report_paths = [pathlib.Path(p) for p in sys.argv[1:]]
products = {}
orgs = {}

for path in report_paths:
    report = json.loads(path.read_text())
    product = report.get("product_surface")
    org_id = report.get("org_id")
    if report.get("status") != "passed":
        raise SystemExit(f"{path.name} did not pass: {json.dumps(report, indent=2)}")
    if not product or not org_id:
        raise SystemExit(f"{path.name} missing product_surface/org_id")
    if product in {"ponti", "pymes"}:
        raise SystemExit(f"{path.name} uses real product fixture {product!r}")
    if product in products:
        raise SystemExit(f"duplicate product_surface {product!r}")
    if org_id in orgs:
        raise SystemExit(f"duplicate org_id {org_id!r}")
    products[product] = path.name
    orgs[org_id] = path.name

required = {"reference", "shadow"}
if set(products) != required:
    raise SystemExit(f"expected products {sorted(required)}, got {sorted(products)}")

print(json.dumps({
    "status": "passed",
    "products": sorted(products),
    "orgs": sorted(orgs),
    "checks": [
        "product_contracts",
        "eval_packs",
        "distinct_product_surfaces",
        "distinct_org_ids",
        "no_real_product_defaults"
    ]
}, indent=2))
PY
