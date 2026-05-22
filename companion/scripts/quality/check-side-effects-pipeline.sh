#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

matches="$(
  cd "$ROOT" && \
    git grep -nE '\.SendWhatsApp(Text|Template)\(' -- '*.go' ':!internal/watchers/pymesclient/*' ':!*_test.go' || true
)"
matches="$(
  printf "%s\n" "$matches" | grep -v '^internal/connectors/registry/' || true
)"

if [ -n "$matches" ]; then
  echo "ERROR: product side effects must go through ConnectorExecution + Nexus governance:" >&2
  echo "$matches" >&2
  exit 1
fi

echo "Side effects pipeline check passed."
