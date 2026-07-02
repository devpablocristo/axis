#!/usr/bin/env bash
# Bloquear cualquier import de packages internos de nexus que solo deben
# existir en nexus/internal/.
#
# La regla del ecosistema es absoluta: la lógica de nexus vive en
# Nexus. Productos consumen via HTTP (nexusclient). Sin excepciones.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

# Patrones prohibidos: internals de Nexus fuera de Nexus.
PROHIBITED='nexus/internal/'

# Buscamos en archivos .go pero excluimos vendor/ y dirs de build.
matches="$(
  cd "$ROOT" && \
    git grep -n "\"github.com/devpablocristo/${PROHIBITED}" -- '*.go' || true
)"

if [ -n "$matches" ]; then
  echo "ERROR: imports prohibidos de kernels internos de nexus encontrados:" >&2
  echo "$matches" >&2
  echo >&2
  echo "La lógica de nexus vive solo en nexus/internal/." >&2
  echo "Productos deben llamar a Nexus via HTTP usando nexusclient." >&2
  exit 1
fi

echo "Nexus imports check passed."
