#!/usr/bin/env sh
set -u

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"
. "$repo_root/scripts/quality/tool-path.sh"

status=0

printf '%s\n' 'Running Squawk (advisory)...'
find v2/bff/migrations v2/companion/migrations v2/nexus/migrations -type f -name '*.sql' -print0 \
  | xargs -0 "$(find_quality_tool squawk)" || status=1

printf '%s\n' 'Running OSV Scanner (advisory)...'
"$(find_quality_tool osv-scanner)" scan -r v2 || status=1

printf '%s\n' 'Running Trivy (advisory)...'
"$(find_quality_tool trivy)" filesystem \
  --scanners vuln,misconfig,secret \
  --severity HIGH,CRITICAL \
  --skip-dirs v2/console/node_modules \
  --skip-files '**/.env' \
  --exit-code 0 \
  v2 || status=1

exit "$status"
