SHELL := /bin/bash
# Honor the local docker-compose.override.yml when present (compose only
# auto-loads it without -f, and make targets pin -f explicitly).
DC := docker compose --project-directory $(CURDIR) -f $(CURDIR)/docker-compose.yml $(if $(wildcard $(CURDIR)/docker-compose.override.yml),-f $(CURDIR)/docker-compose.override.yml,)

.PHONY: ensure-product-network up down seed-demo test-console-e2e test-approval-flow-e2e test-console-real-e2e test-e2e

ensure-product-network:
	@docker network inspect axis-medmory >/dev/null 2>&1 || docker network create axis-medmory >/dev/null

up: ensure-product-network
	@test -f .env || cp .env.example .env
	$(DC) up -d --build

down:
	$(DC) down

seed-demo: ensure-product-network
	@test -f .env || cp .env.example .env
	$(DC) up -d --build bff-v2 console-v2
	$(DC) run --rm -T --build seed-demo

test-console-e2e: ensure-product-network
	@test -f .env || cp .env.example .env
	BFF_V2_IDENTITY_PROVIDER=dev $(DC) up -d --build bff-v2 console-v2-e2e
	BFF_V2_IDENTITY_PROVIDER=dev $(DC) run --rm console-e2e sh -lc 'npm run test:e2e'

test-approval-flow-e2e: ensure-product-network
	@test -f .env || cp .env.example .env
	BFF_V2_IDENTITY_PROVIDER=dev $(DC) up -d --build bff-v2
	BFF_V2_IDENTITY_PROVIDER=dev $(DC) run --rm --build approval-flow-e2e

test-console-real-e2e: ensure-product-network
	@test -f .env || cp .env.example .env
	BFF_V2_IDENTITY_PROVIDER=dev $(DC) up -d --build bff-v2 console-v2-e2e
	BFF_V2_IDENTITY_PROVIDER=dev $(DC) run --rm -e AXIS_REAL_E2E=1 console-e2e sh -lc 'npx playwright test e2e/real-approval-flow.spec.ts'

test-e2e: test-console-e2e test-approval-flow-e2e test-console-real-e2e
