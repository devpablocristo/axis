SHELL := /bin/bash
DC := docker compose --project-directory $(CURDIR) -f $(CURDIR)/docker-compose.yml

.PHONY: up down

up:
	@test -f .env || cp .env.example .env
	$(DC) up -d --build

down:
	$(DC) down
