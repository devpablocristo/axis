#!/usr/bin/env bash
# Bloquear cualquier import de packages internos de governance que solo deben
# existir en nexus/governance/internal/.
#
# La regla del ecosistema es absoluta: la lógica de governance vive en
# Nexus. Productos consumen via HTTP (governanceclient). Sin excepciones.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

# Patrones prohibidos: lógica interna de governance fuera de Nexus.
PROHIBITED='platform/kernels/governance/go/(decision|policy|risk|approval|kernel)'

# Buscamos en archivos .go pero excluimos vendor/ y dirs de build.
matches="$(
  cd "$ROOT" && \
    git grep -nE "\"github\\.com/devpablocristo/${PROHIBITED}" -- '*.go' || true
)"

if [ -n "$matches" ]; then
  echo "ERROR: imports prohibidos de kernels internos de governance encontrados:" >&2
  echo "$matches" >&2
  echo >&2
  echo "La lógica de governance vive solo en nexus/governance/internal/." >&2
  echo "Productos deben llamar a Nexus via HTTP usando governanceclient." >&2
  exit 1
fi

echo "Governance imports check passed."
