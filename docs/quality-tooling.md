# Quality tooling

Axis uses a layered quality gate for v2.

## Local setup

Install the versioned Git hook once per clone:

```sh
./scripts/quality/install-hooks.sh
```

The pre-commit hook blocks commits when Drift finds stale documentation or
Gitleaks finds a secret in staged changes.

Run all blocking checks manually with:

```sh
./scripts/quality/check-blocking.sh
```

Run advisory database, dependency, and filesystem scans with:

```sh
./scripts/quality/check-reporting.sh
```

## CI policy

- Blocking: Drift, Gitleaks, Semgrep ERROR findings, Govulncheck,
  GolangCI-Lint, Go tests, Console typecheck, and Playwright E2E.
- Advisory: Squawk, OSV Scanner, and Trivy. These publish findings without
  blocking the pull request while the baseline is being hardened.
- Codebase Memory is refreshed through its MCP after structural changes. It is
  not a CI dependency because the graph service is external to the repository.
