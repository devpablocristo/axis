#!/usr/bin/env bash
# Ejecutar governance localmente contra el postgres de docker compose
set -euo pipefail

export DATABASE_URL="${DATABASE_URL:-postgres://postgres:postgres@localhost:15434/nexus?sslmode=disable}"
export GOVERNANCE_API_KEYS="${GOVERNANCE_API_KEYS:-admin=governance-admin-dev-key}"
export PORT="${PORT:-8080}"

cd "$(dirname "$0")/../../governance"
echo "Starting governance on :$PORT..."
go run ./cmd/api/
