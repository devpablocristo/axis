# Repository Guidelines

## Project Structure & Module Organization

Axis is organized into `v1/` and `v2/`; new work should target `v2/` unless a task explicitly concerns legacy behavior. Each version contains Go services (`bff/`, `companion/`, and `nexus/`) plus a React/TypeScript console in `console/`. Service tests live beside implementation files as `*_test.go`. Console end-to-end tests are under `v2/console/e2e/`. Shared documentation belongs in `docs/` or `v2/docs/`, while repository-wide automation and security checks live in `scripts/quality/`.

## Build, Test, and Development Commands

Run v2 integration workflows from `v2/`:

- `make up` builds and starts the local Docker stack.
- `make down` stops it.
- `make seed-demo` provisions representative demo data.
- `make test-console-e2e` runs Playwright against Docker services.
- `make test-approval-flow-e2e` validates the governed approval path.
- `make test-e2e` runs the complete v2 E2E suite.

For focused work, run `go test ./...` inside `v2/bff`, `v2/companion`, or `v2/nexus`. In `v2/console`, use `npm ci`, `npm run dev`, `npm run typecheck`, and `npm run build`. Before submitting, run `./scripts/quality/check-blocking.sh` from the repository root.

## Coding Style & Naming Conventions

Format Go with `gofmt`; keep package names lowercase and exported identifiers in PascalCase. Use idiomatic table-driven tests where several cases share setup. TypeScript uses two-space indentation, PascalCase React components (`VirployeesPage.tsx`), camelCase functions, and explicit types at API boundaries. Prefer existing shared UI and lifecycle packages over duplicating components or state rules.

## Testing Guidelines

Name Go tests `TestBehaviorCondition` and Playwright files `*.spec.ts`. Add unit tests beside changed Go code and E2E coverage for user-visible or cross-service flows. Tenant isolation, authorization, lifecycle transitions, and sensitive-data redaction require explicit regression cases. Docker is the authoritative environment for integration and browser tests.

## Commit & Pull Request Guidelines

Recent history favors short imperative subjects, sometimes using Conventional Commit scopes such as `feat(console): ...`. Keep each commit focused. Pull requests should explain behavior and risk, list verification commands, link the relevant issue or design note, and include screenshots for console changes. Call out migrations, environment changes, and security implications explicitly.

## Security & Agent Notes

Never commit `.env` files, credentials, tokens, or unredacted sensitive content. Preserve tenant boundaries in every query and handler. For code discovery, prefer the codebase knowledge-graph tools before broad text searches, and update drift documentation when modifying anchored behavior.
