#!/usr/bin/env sh
set -eu

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"
. "$repo_root/scripts/quality/tool-path.sh"

"$(find_quality_tool drift)" check
"$(find_quality_tool gitleaks)" git --redact --no-banner "$repo_root"
"$(find_quality_tool semgrep)" scan --config p/default --config p/secrets --severity ERROR --error --no-git-ignore v2

for module in v2/bff v2/companion v2/nexus; do
  (cd "$repo_root/$module" && "$(find_quality_tool govulncheck)" ./...)
  (cd "$repo_root/$module" && "$(find_quality_tool golangci-lint)" run ./...)
done
