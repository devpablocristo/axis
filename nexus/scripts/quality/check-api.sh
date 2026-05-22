#!/usr/bin/env bash
# Verificar calidad del stack.
set -euo pipefail

NEXUS_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
AXIS_ROOT="$(cd "$NEXUS_ROOT/.." && pwd)"
GO_IN_ENV="$NEXUS_ROOT/scripts/quality/go-in-env.sh"

echo "=== migrations ==="
bash "$NEXUS_ROOT/scripts/quality/check-migrations.sh"

echo "=== docker compose config ==="
docker compose --project-directory "$AXIS_ROOT" -f "$AXIS_ROOT/docker-compose.yml" config --services >/dev/null

echo "=== nexus go build ==="
"$GO_IN_ENV" . build ./...

echo "=== nexus go vet ==="
"$GO_IN_ENV" . vet ./...

echo "=== nexus go test ==="
"$GO_IN_ENV" . test ./... -count=1 -race

if [ -d "$AXIS_ROOT/console/node_modules" ]; then
  echo "=== console typecheck ==="
  cd "$AXIS_ROOT/console"
  npm run typecheck

  echo "=== console build ==="
  npm run build
else
  echo "Skipping console checks: node_modules not installed. Run npm ci in axis/console to enable them."
fi

echo ""
echo "Quality checks passed."
