# Nexus

Nexus es el servicio headless de governance de Axis. Decide
`allow`/`deny`/`require_approval`, administra approvals, policies, action types,
delegations, RBAC, audit y evidence packs. No incluye runtime LLM, memoria IA ni
UI propia.

## Estructura

```text
nexus/
├── governance/          # servicio Go
├── scripts/             # quality, smoke, e2e
├── docker-compose.yml   # governance + postgres
├── docker-compose.dev.yml
├── Makefile
└── .env.example
```

## Contrato

- HTTP API en `governance/`.
- Auth inbound: `X-API-Key` o Bearer JWT/OIDC interno.
- Datos tenant-owned requieren `org_id` no vacío.
- Config compartida explícita: `policies`, `action_types` y `delegations`
  pueden ser globales o de tenant. La escritura de globales requiere
  `nexus:cross_org`.
- La UI administrativa vive en `../console` y accede por `../bff`.

## Arranque local

```bash
test -f .env || cp .env.example .env
make up
```

URL por defecto: `http://localhost:18084`.

## Tests

```bash
make test
make qa
make smoke
make e2e
```

## Deploy

Nexus se deploya como componente independiente de Axis. Usar tags `nexus-v*`
para releases del servicio.
