# Companion (proyecto independiente, ex-v3/companion en monorepo Nexus)
.PHONY: test qa check-migrations check-governance-imports smoke up down build logs dev dev-down

DC := docker compose --project-directory $(CURDIR) -f $(CURDIR)/docker-compose.yml

# --- Quality ---
check-migrations:
	bash scripts/quality/check-migrations.sh

check-governance-imports:
	bash scripts/quality/check-governance-imports.sh

test:
	go test ./... -count=1

qa: check-migrations check-governance-imports
	go build ./...
	go vet ./...
	go test ./... -count=1 -race

# --- Docker ---
up:
	@test -f .env || cp .env.example .env
	$(DC) up -d --build

down:
	$(DC) down

build:
	$(DC) build

logs:
	$(DC) logs -f

# --- Tests contra API corriendo ---
smoke:
	bash scripts/smoke/run-companion-review-flow.sh
	bash scripts/smoke/run-companion-execution-flow.sh
	bash scripts/smoke/run-companion-denied-flow.sh
	bash scripts/smoke/run-companion-governance-assist-flow.sh
