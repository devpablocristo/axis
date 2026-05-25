#!/usr/bin/env bash
# Ejecutar nexus localmente contra el postgres de docker compose
set -euo pipefail

export DATABASE_URL="${DATABASE_URL:-postgres://postgres:postgres@localhost:15434/nexus?sslmode=disable}"
export NEXUS_API_KEYS="${NEXUS_API_KEYS:-admin=nexus-admin-dev-key|service_principal=true|org_id=local-dev-org|scopes=nexus:requests:read+nexus:requests:write+nexus:requests:result+nexus:approvals:decide+nexus:policies:admin+nexus:rbac:admin+nexus:evidence:write+nexus:findings:read+nexus:findings:write+nexus:dashboard:read+nexus:learning:propose+nexus:cross_org,admin-a=nexus-admin-a-dev-key|actor=nexus-admin-a|role=admin|service_principal=true|org_id=local-dev-org|scopes=nexus:requests:read+nexus:requests:write+nexus:policies:admin,admin-b=nexus-admin-b-dev-key|actor=nexus-admin-b|role=admin|service_principal=true|org_id=local-dev-org|scopes=nexus:requests:read+nexus:requests:write+nexus:policies:admin,admin-other=nexus-admin-other-dev-key|actor=nexus-admin-other|role=admin|service_principal=true|org_id=other-dev-org|scopes=nexus:requests:read+nexus:requests:write+nexus:policies:admin,ponti=nexus-ponti-dev-key|actor=ponti-backend|role=service|service_principal=true|org_id=local-dev-org|scopes=nexus:requests:read+nexus:requests:write+nexus:requests:result}"
export PORT="${PORT:-8080}"

cd "$(dirname "$0")/../.."
echo "Starting Nexus on :$PORT..."
go run ./cmd/api/
