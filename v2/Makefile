SHELL := /bin/bash
DC := docker compose --project-directory $(CURDIR) -f $(CURDIR)/docker-compose.yml

.PHONY: up down test-console-e2e test-approval-flow-e2e test-console-real-e2e test-e2e

up:
	@test -f .env || cp .env.example .env
	$(DC) up -d --build

down:
	$(DC) down

test-console-e2e:
	@test -f .env || cp .env.example .env
	$(DC) up -d --build bff-v2 console-v2-e2e
	$(DC) run --rm console-e2e sh -lc 'npm run test:e2e'

test-approval-flow-e2e:
	@test -f .env || cp .env.example .env
	$(DC) up -d --build bff-v2
	$(DC) run --rm --build approval-flow-e2e

test-console-real-e2e:
	@test -f .env || cp .env.example .env
	$(DC) up -d --build bff-v2 console-v2-e2e
	$(DC) run --rm -e AXIS_REAL_E2E=1 console-e2e sh -lc 'npx playwright test e2e/real-approval-flow.spec.ts'

test-e2e: test-console-e2e test-approval-flow-e2e test-console-real-e2e
