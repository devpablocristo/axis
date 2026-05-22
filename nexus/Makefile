# Nexus
.PHONY: test qa check-migrations smoke e2e acceptance up down build logs dev dev-logs dev-down up-ponti-local down-ponti-local logs-ponti-local

DC := docker compose --project-directory $(CURDIR) -f $(CURDIR)/docker-compose.yml
DC_DEV := $(DC) -f $(CURDIR)/docker-compose.dev.yml
DC_PONTI := $(DC) -f $(CURDIR)/docker-compose.ponti.yml

# --- Quality ---
check-migrations:
	bash scripts/quality/check-migrations.sh

test:
	bash scripts/quality/go-in-env.sh governance test ./... -count=1

qa: check-migrations
	bash scripts/quality/check-api.sh

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
	bash scripts/smoke/run-policies-crud.sh
	bash scripts/smoke/run-requests-flow.sh

e2e:
	bash scripts/e2e/run-full-lifecycle.sh

acceptance: smoke e2e

# --- Dev (hot reload) ---
dev:
	@test -f .env || cp .env.example .env
	$(DC_DEV) up -d --build

dev-logs:
	$(DC_DEV) logs -f

dev-down:
	$(DC_DEV) down

# --- Ponti local (infra + nexus + ponti-backend + ponti-ai + frontend) ---
# Levanta todo el stack necesario para probar ponti contra Nexus local.
# Primera ejecucion puede tardar varios minutos (yarn install, go build, pip install).
up-ponti-local:
	@test -f .env || cp .env.example .env
	$(DC_PONTI) up -d --build
	@bash scripts/seed-ponti-policies.sh
	@echo ""
	@echo "Stack listo:"
	@echo "  - Nexus governance : http://localhost:18084"
	@echo "  - Ponti backend    : http://localhost:8080"
	@echo "  - Ponti AI         : http://localhost:8090"
	@echo "  - Ponti BFF        : http://localhost:3000"
	@echo "  - Ponti UI         : http://localhost:5173"

down-ponti-local:
	$(DC_PONTI) down

logs-ponti-local:
	$(DC_PONTI) logs -f ponti-backend ponti-ai ponti-ui ponti-bff governance
