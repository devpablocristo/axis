#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

go run ./cmd/product-onboarding-check \
  -contract scripts/onboarding/reference-product-contract.json \
  -eval-pack scripts/evals/reference-golden.json
