#!/usr/bin/env sh
set -eu

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"
. "$repo_root/scripts/quality/tool-path.sh"

drift_bin="$(find_quality_tool drift)"
gitleaks_bin="$(find_quality_tool gitleaks)"

printf '%s\n' 'Checking documentation anchors...'
"$drift_bin" check

printf '%s\n' 'Checking staged changes for secrets...'
"$gitleaks_bin" git --pre-commit --staged --redact --no-banner "$repo_root"
