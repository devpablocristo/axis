#!/usr/bin/env bash
# Validate embedded migration filenames before running build/test jobs.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

check_dir() {
  local dir="$1"
  local duplicates
  duplicates="$(
    find "$dir" -maxdepth 1 -type f -name '*.up.sql' -printf '%f\n' |
      sort |
      sed -E 's/^([0-9]+).*/\1/' |
      uniq -d
  )"
  if [ -n "$duplicates" ]; then
    echo "Duplicate migration versions in $dir:" >&2
    echo "$duplicates" >&2
    return 1
  fi
}

check_dir "$ROOT/migrations"

echo "Migration version checks passed."
