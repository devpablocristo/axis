#!/usr/bin/env bash
# Ejecutar nexus localmente contra el postgres de docker compose
set -euo pipefail

export DATABASE_URL="${DATABASE_URL:-postgres://postgres:postgres@localhost:15434/nexus?sslmode=disable}"
export NEXUS_API_KEYS="${NEXUS_API_KEYS:-admin=nexus-admin-dev-key}"
export PORT="${PORT:-8080}"

cd "$(dirname "$0")/../.."
echo "Starting Nexus on :$PORT..."
go run ./cmd/api/
