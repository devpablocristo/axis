#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

if [ -n "${SECURITY_EVAL_REPORT:-}" ]; then
  go test -json ./internal/securityevals | tee "$SECURITY_EVAL_REPORT"
else
  go test ./internal/securityevals
fi
