#!/usr/bin/env bash
# Crea la base companion si no existe (útil cuando el volumen de Postgres
# se creó antes de montar postgres-init).
set -euo pipefail

V3_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$V3_ROOT"

if ! docker compose exec -T companion-postgres pg_isready -U postgres >/dev/null 2>&1; then
  echo "ERROR: Postgres no responde. Ejecutá: docker compose up -d companion-postgres" >&2
  exit 1
fi

EXISTS=$(docker compose exec -T companion-postgres psql -U postgres -Atqc \
  "SELECT 1 FROM pg_database WHERE datname = 'companion'" || true)
if [ "$EXISTS" = "1" ]; then
  echo "OK: database companion already exists"
  exit 0
fi

echo "Creating database companion..."
docker compose exec -T companion-postgres psql -U postgres -c "CREATE DATABASE companion;"
echo "OK: companion created"
