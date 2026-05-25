SHELL := /bin/bash
DC := docker compose --project-directory $(CURDIR) -f $(CURDIR)/docker-compose.yml

.PHONY: test test-companion test-nexus test-bff test-console \
	qa qa-companion qa-nexus check-companion check-nexus hygiene \
	smoke smoke-companion smoke-nexus e2e-nexus e2e-nexus-policy-promotion acceptance-nexus \
	dev-apis dev-companion dev-nexus \
	network up down build logs compose-services

test: hygiene test-companion test-nexus test-bff

qa: hygiene qa-companion qa-nexus test-bff test-console

hygiene:
	@python3 -c 'import pathlib,sys; roots=[pathlib.Path("companion"),pathlib.Path("nexus")]; banned_names={".cursor",".claude",".air.toml",".env.example",".dockerignore",".gitignore","renovate.json","AGENTS.md","CLAUDE.md"}; bad=[]; [bad.append(str(p)) for r in roots for p in r.iterdir() if p.name in banned_names]; [bad.append(str(p)) for r in roots for p in r.glob("*.md") if p.name!="README.md"]; [bad.append(str(p)) for r in roots for p in r.rglob("*") if p.name=="Makefile" or p.name.startswith("Dockerfile") or p.name.startswith("docker-compose")]; print("\n".join(bad)); sys.exit(1 if bad else 0)'
	@python3 -c 'import subprocess,sys; ac="apps/"+"console"; deny=["infra/"+ "docker","Cl"+"erk","VITE_"+"CLERK","COMPANION_"+"CONSOLE_PORT","NEXUS_"+"CONSOLE_PORT","Companion "+"UI","Nexus "+"console","axis/"+ac,ac,"130"+"01","130"+"02"]; cmd=["rg","-n","--hidden","|".join(deny),".","-g","!.git/**","-g","!**/node_modules/**","-g","!**/dist/**","-g","!Makefile"]; out=subprocess.run(cmd,text=True,capture_output=True); print(out.stdout,end=""); sys.exit(1 if out.stdout else 0)'

check-companion:
	cd companion && bash scripts/quality/check-migrations.sh
	cd companion && bash scripts/quality/check-nexus-imports.sh
	cd companion && bash scripts/quality/check-side-effects-pipeline.sh
	cd companion && bash scripts/evals/run-security-evals.sh

test-companion:
	cd companion && go test ./... -count=1

qa-companion: check-companion
	cd companion && go build ./...
	cd companion && go vet ./...
	cd companion && go test ./... -count=1 -race

check-nexus:
	cd nexus && bash scripts/quality/check-migrations.sh
	cd nexus && bash scripts/quality/check-no-ai-runtime.sh

test-nexus:
	cd nexus && go test ./... -count=1

qa-nexus: check-nexus
	cd nexus && go build ./...
	cd nexus && go vet ./...
	cd nexus && go test ./... -count=1 -race

test-bff:
	cd bff && go test ./...

test-console:
	cd console && npm run typecheck && npm run build

smoke: smoke-nexus smoke-companion

smoke-companion:
	cd companion && bash scripts/smoke/run-companion-nexus-flow.sh
	cd companion && bash scripts/smoke/run-companion-execution-flow.sh
	cd companion && bash scripts/smoke/run-companion-denied-flow.sh
	cd companion && bash scripts/smoke/run-companion-nexus-assist-flow.sh

smoke-nexus:
	cd nexus && bash scripts/smoke/run-policies-crud.sh
	cd nexus && bash scripts/smoke/run-requests-flow.sh

e2e-nexus:
	cd nexus && bash scripts/e2e/run-full-lifecycle.sh
	cd nexus && bash scripts/e2e/run-policy-promotion-sod.sh

e2e-nexus-policy-promotion:
	cd nexus && bash scripts/e2e/run-policy-promotion-sod.sh

acceptance-nexus: smoke-nexus e2e-nexus

dev-nexus:
	@test -f .env || cp .env.example .env
	@set -a; source .env; set +a; \
	export PORT="$${NEXUS_PORT:-18084}"; \
	export DATABASE_URL="$${NEXUS_DATABASE_URL:-postgres://postgres:postgres@localhost:$${NEXUS_POSTGRES_PORT:-15434}/nexus?sslmode=disable}"; \
	export NEXUS_API_KEYS="admin=$${NEXUS_ADMIN_API_KEY:-nexus-admin-dev-key}|service_principal=true|org_id=$${AXIS_DEV_ORG_ID:-local-dev-org}|scopes=nexus:requests:read+nexus:requests:write+nexus:requests:result+nexus:approvals:decide+nexus:policies:admin+nexus:rbac:admin+nexus:evidence:write+nexus:findings:read+nexus:findings:write+nexus:dashboard:read+nexus:learning:propose+nexus:cross_org,admin-a=$${NEXUS_ADMIN_A_API_KEY:-nexus-admin-a-dev-key}|actor=nexus-admin-a|role=admin|service_principal=true|org_id=$${AXIS_DEV_ORG_ID:-local-dev-org}|scopes=nexus:requests:read+nexus:requests:write+nexus:policies:admin,admin-b=$${NEXUS_ADMIN_B_API_KEY:-nexus-admin-b-dev-key}|actor=nexus-admin-b|role=admin|service_principal=true|org_id=$${AXIS_DEV_ORG_ID:-local-dev-org}|scopes=nexus:requests:read+nexus:requests:write+nexus:policies:admin,admin-other=$${NEXUS_OTHER_ORG_ADMIN_API_KEY:-nexus-admin-other-dev-key}|actor=nexus-admin-other|role=admin|service_principal=true|org_id=$${AXIS_OTHER_ORG_ID:-other-dev-org}|scopes=nexus:requests:read+nexus:requests:write+nexus:policies:admin,argos=$${NEXUS_ARGOS_API_KEY:-argos-nexus-dev-key}|service_principal=true|org_id=$${ARGOS_ORG_ID:-argos-local-org}|scopes=nexus:findings:read+nexus:findings:write"; \
	export NEXUS_INTERNAL_JWT_SECRET="$${AXIS_INTERNAL_JWT_SECRET:-axis-dev-internal-jwt-secret-change-me}"; \
	export NEXUS_INTERNAL_JWT_ISSUER="$${AXIS_INTERNAL_JWT_ISSUER:-axis-bff}"; \
	export NEXUS_INTERNAL_JWT_AUDIENCE="$${NEXUS_INTERNAL_JWT_AUDIENCE:-nexus}"; \
	export APPROVAL_DEFAULT_TTL="$${NEXUS_APPROVAL_TTL:-3600}"; \
	air -c air.nexus.toml

dev-companion:
	@test -f .env || cp .env.example .env
	@set -a; source .env; set +a; \
	nexus_port="$${NEXUS_PORT:-18084}"; \
	export PORT="$${COMPANION_PORT:-18085}"; \
	export DATABASE_URL="$${COMPANION_DATABASE_URL:-postgres://postgres:postgres@localhost:$${COMPANION_POSTGRES_PORT:-15435}/companion?sslmode=disable}"; \
	export COMPANION_API_KEYS="admin=$${COMPANION_ADMIN_API_KEY:-companion-admin-dev-key}|service_principal=true|org_id=$${AXIS_DEV_ORG_ID:-local-dev-org}|scopes=companion:tasks:read+companion:tasks:write+companion:connectors:execute+companion:connectors:admin+companion:watchers:read+companion:watchers:write+companion:watchers:execute+companion:nexus:read+companion:nexus:admin+companion:nexus-assist:read+companion:nexus-assist:admin+companion:assist:read+companion:assist:write+companion:cross_org,argos=$${COMPANION_ARGOS_API_KEY:-argos-companion-dev-key}|service_principal=true|org_id=$${ARGOS_ORG_ID:-argos-local-org}|scopes=companion:assist:read+companion:assist:write"; \
	export COMPANION_INTERNAL_JWT_SECRET="$${AXIS_INTERNAL_JWT_SECRET:-axis-dev-internal-jwt-secret-change-me}"; \
	export COMPANION_INTERNAL_JWT_ISSUER="$${AXIS_INTERNAL_JWT_ISSUER:-axis-bff}"; \
	export COMPANION_INTERNAL_JWT_AUDIENCE="$${COMPANION_INTERNAL_JWT_AUDIENCE:-companion}"; \
	export NEXUS_BASE_URL="$${NEXUS_BASE_URL:-http://localhost:$$nexus_port}"; \
	export NEXUS_API_KEY="$${NEXUS_API_KEY:-$${NEXUS_ADMIN_API_KEY:-nexus-admin-dev-key}}"; \
	export COMPANION_LLM_PROVIDER="$${COMPANION_LLM_PROVIDER:-fake}"; \
	export COMPANION_LLM_MODEL="$${COMPANION_LLM_MODEL:-fake}"; \
	air -c air.companion.toml

dev-apis:
	@test -f .env || cp .env.example .env
	+$(MAKE) -j2 dev-nexus dev-companion

network:
	@docker network inspect axis-local >/dev/null 2>&1 || docker network create axis-local >/dev/null

up: network
	@test -f .env || cp .env.example .env
	$(DC) up -d --build

down:
	$(DC) down

build:
	$(DC) build

logs:
	$(DC) logs -f

compose-services:
	$(DC) config --services
