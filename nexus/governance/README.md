# Nexus Governance

Servicio Go headless de governance para Axis.

## Responsabilidades

- Evaluar requests gobernadas y decidir `allow`, `deny` o `require_approval`.
- Administrar approvals, policies, action types, delegations, RBAC, audit,
  evidence packs y result reports.
- Exigir `org_id` no vacío para datos operativos tenant-owned.
- Permitir config global solo como shared config explícita y administrada con
  `nexus:cross_org`.

## Auth

Inbound soportado:

- `X-API-Key` para service-to-service.
- Bearer JWT OIDC/JWKS.
- Bearer JWT interno HMAC emitido por Axis BFF.

Variables:

- `GOVERNANCE_API_KEYS`
- `GOVERNANCE_AUTH_ISSUER_URL`
- `GOVERNANCE_AUTH_AUDIENCE`
- `GOVERNANCE_INTERNAL_JWT_SECRET`
- `GOVERNANCE_INTERNAL_JWT_ISSUER`
- `GOVERNANCE_INTERNAL_JWT_AUDIENCE`

## Tests

Desde `nexus/`:

```bash
make test
make qa
make smoke
make e2e
```
