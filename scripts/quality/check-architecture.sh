#!/usr/bin/env sh
set -eu

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

fail_matches() {
  message="$1"
  matches="$2"
  if [ -n "$matches" ]; then
    echo "$message" >&2
    echo "$matches" >&2
    exit 1
  fi
}

# Application/domain packages own ports. Only composition roots and outbound
# adapter packages may depend on concrete adapters.
core_adapter_imports="$(
  rg -n '"[^"]*/internal/adapters/' \
    v2/bff/internal v2/companion/internal v2/nexus/internal v2/runtime/internal v2/artifact-worker/internal \
    --glob '!**/adapters/**' --glob '!**/*_test.go' || true
)"
fail_matches "core packages must not import outbound adapters" "$core_adapter_imports"

# A Go service may not compile another Axis service into its process.
for module in bff companion nexus runtime artifact-worker; do
  own="github.com/devpablocristo/$module-v2"
  cross_imports="$(
    rg -n '"github\.com/devpablocristo/(bff|companion|nexus|runtime|artifact-worker)-v2/' "v2/$module" \
      --glob '*.go' | rg -v "\"$own/" || true
  )"
  fail_matches "v2/$module imports another Axis product directly" "$cross_imports"
done

# Provider SDKs and operating-system execution belong only in outbound
# adapters, never in the application workflow.
runtime_provider_imports="$(
  rg -n 'platform/kernels/ai' v2/runtime/internal \
    --glob '*.go' --glob '!**/adapters/out/modelkernel/**' || true
)"
fail_matches "Runtime core depends on a concrete model provider" "$runtime_provider_imports"

artifact_process_imports="$(
  rg -n '"os/exec"' v2/artifact-worker/internal \
    --glob '*.go' --glob '!**/adapters/out/processrunner/**' || true
)"
fail_matches "Artifact Worker core executes operating-system commands directly" "$artifact_process_imports"

# Domain packs and historical decoders are allowed in their explicit
# extension/legacy locations only.
domain_literals="$(
  rg -n -i 'calendar\.|clinical\.|medmory' \
    v2/bff/internal/productedge \
    v2/bff/internal/productintegrations \
    v2/companion/internal/virployees \
    v2/runtime/internal/planner \
    v2/nexus/internal/productintegrations \
    v2/console/src \
    --glob '!**/*_test.go' \
    --glob '!**/legacy_*.go' \
    --glob '!v2/console/src/legacy/**' \
    --glob '!v2/companion/internal/virployees/preparedactions/legacy/**' || true
)"
fail_matches "generic core contains a product or domain-specific literal" "$domain_literals"

clerk_imports="$(
  rg -n '@clerk/' v2/console/src \
    --glob '*.{ts,tsx}' \
    --glob '!**/auth/clerk/**' \
    --glob '!**/auth/clerkAuthAdapter.tsx' || true
)"
fail_matches "Console imports Clerk outside its AuthPort adapter" "$clerk_imports"

topology_keys="$(
  rg -n '"(services|required_services|bff|companion|nexus)"' \
    v2/contracts/schemas/product-integration.v3.schema.json || true
)"
fail_matches "product integration v3 exposes Axis service topology" "$topology_keys"

for required_contract in \
  v2/contracts/schemas/product-integration.v3.schema.json \
  v2/contracts/schemas/invocation-context.v1.schema.json \
  v2/contracts/schemas/prepared-action.v2.schema.json \
  v2/contracts/schemas/connector.v1.schema.json \
  v2/contracts/openapi/bff-facade.v1.yaml \
  v2/contracts/openapi/governance-internal.v1.yaml \
  v2/contracts/openapi/runtime-internal.v1.yaml \
  v2/contracts/openapi/artifact-extraction.v1.yaml \
  v2/contracts/openapi/connector.v1.yaml
do
  if [ ! -s "$required_contract" ]; then
    echo "missing required contract: $required_contract" >&2
    exit 1
  fi
done

echo "architecture boundaries: OK"
