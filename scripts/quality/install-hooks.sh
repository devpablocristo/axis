#!/usr/bin/env sh
set -eu

repo_root="$(git rev-parse --show-toplevel)"
git -C "$repo_root" config core.hooksPath .githooks
chmod +x "$repo_root/.githooks/pre-commit" "$repo_root/scripts/quality/"*.sh
printf '%s\n' 'Axis Git hooks installed from .githooks.'
