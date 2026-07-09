SHELL := /bin/bash
DC := docker compose --project-directory $(CURDIR) -f $(CURDIR)/docker-compose.yml

.PHONY: up down test-console-e2e

up:
	@test -f .env || cp .env.example .env
	$(DC) up -d --build

down:
	$(DC) down

test-console-e2e:
	@test -f .env || cp .env.example .env
	$(DC) up -d --build bff-v2 console-v2-e2e
	$(DC) run --rm console-e2e
