#!/usr/bin/env bash
set -euo pipefail

ROOT="${1:-governance}"

patterns=(
  "platform/kernels/ai"
	"go-openai"
  "anthropic"
  "generative-ai-go"
  "ollama"
  "langchaingo"
)

for pattern in "${patterns[@]}"; do
  if rg -n --glob '*.go' -- "$pattern" "$ROOT"; then
    echo "Nexus must not import or embed AI runtime/provider code: matched '$pattern'" >&2
    exit 1
  fi
done

echo "No AI runtime check passed."
