# Nexus

Servicio headless de Axis para decisiones sensibles. Decide `allow`/`deny`/
`require_approval`, administra approvals, policies, action types, delegations,
RBAC, audit y evidence packs. No incluye runtime LLM, memoria IA ni UI propia.

## Estructura

```text
nexus/
├── cmd/api/
├── internal/
├── migrations/
├── wire/
├── scripts/
├── docs/
├── go.mod
├── go.sum
└── openapi.yaml
```

Docker, compose y Make targets viven en la raíz de Axis.

## Contrato

- Auth inbound: `X-API-Key` o Bearer JWT/OIDC interno.
- Datos tenant-owned requieren `org_id` no vacío.
- Config compartida explícita: `policies`, `action_types` y `delegations`
  pueden ser globales o de tenant. La escritura de globales requiere
  `nexus:cross_org`.
- La UI administrativa vive en `../console` y accede por `../bff`.

## Desarrollo

Desde la raíz de Axis:

```bash
make test-nexus
make qa-nexus
make dev-nexus
make smoke-nexus
make e2e-nexus
make e2e-nexus-policy-promotion
docker compose up -d --build nexus-postgres nexus
```

URL por defecto: `http://localhost:18084`.

Para validar promociones gobernadas con Separation of Duties, el entorno local
expone `NEXUS_ADMIN_A_API_KEY` y `NEXUS_ADMIN_B_API_KEY` como dos actores admin
distintos dentro de la misma org. El self-approval esperado devuelve `409` y el
happy path completo se cubre con `make e2e-nexus-policy-promotion`.

## Documentación

- `nexus/development.md`
- `nexus/enterprise-governance-hardening.md`
- `../nexus/openapi.yaml`
