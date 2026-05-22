#!/usr/bin/env bash
# Run Go commands locally when available, otherwise through the pinned Go Docker image.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
GO_IMAGE="${GO_IMAGE:-golang:1.26.1}"

if [ "$#" -lt 2 ]; then
  echo "usage: $0 <module-dir> <go-args...>" >&2
  exit 2
fi

MODULE_DIR="$1"
shift

if command -v go >/dev/null 2>&1; then
  cd "$ROOT/$MODULE_DIR"
  exec go "$@"
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "go is not installed and docker is not available for fallback" >&2
  exit 127
fi

cid="$(
  docker create \
  -v "$ROOT:/workspace" \
  -w "/workspace/$MODULE_DIR" \
  -e GOCACHE=/tmp/gocache \
  -e GOMODCACHE=/tmp/gomodcache \
  "$GO_IMAGE" go "$@"
)"

cleanup() {
  docker rm -f "$cid" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker start "$cid" >/dev/null
code="$(docker wait "$cid")"
docker logs "$cid"
exit "$code"
