#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

go test ./internal/productevals ./internal/productcontracts ./internal/runtime -run 'Test(EvaluatePack|LoadPack|LoadRepositoryProductEvalPacks|ValidateSpec|Evals_)' -count=1
