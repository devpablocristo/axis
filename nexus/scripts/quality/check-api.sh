#!/usr/bin/env bash
# Verificar calidad del stack.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
GO_IN_ENV="$ROOT/scripts/quality/go-in-env.sh"

echo "=== migrations ==="
bash "$ROOT/scripts/quality/check-migrations.sh"

echo "=== docker compose config ==="
docker compose --project-directory "$ROOT" -f "$ROOT/docker-compose.yml" config --services >/dev/null

echo "=== governance go build ==="
"$GO_IN_ENV" governance build ./...

echo "=== governance go vet ==="
"$GO_IN_ENV" governance vet ./...

echo "=== governance go test ==="
"$GO_IN_ENV" governance test ./... -count=1 -race

if [ -d "$ROOT/console/node_modules" ]; then
  echo "=== console typecheck ==="
  cd "$ROOT/console"
  npm run typecheck

  echo "=== console build ==="
  npm run build
else
  echo "Skipping console checks: node_modules not installed. Run npm ci in console/ to enable them."
fi

echo ""
echo "Quality checks passed."
