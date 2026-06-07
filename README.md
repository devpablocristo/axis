# Axis

Monorepo de control operativo para IA, decisiones sensibles y consola admin/ops.

Axis agrupa deployables independientes. Vivir en el mismo repo no implica mismo
runtime, misma base de datos ni mismo ciclo de deploy.

## Estructura

```text
axis/
├── companion/   # servicio headless de IA/runtime/tools/memory
├── nexus/       # servicio headless de policies/approvals/audit
├── bff/         # backend-for-frontend de la consola operativa
├── console/     # UI admin/ops de Axis
└── packages/    # contratos, auth, UI compartida
```

## Deployables

| Carpeta | Deployable | Rol |
|---|---|---|
| `companion/` | `axis-companion` | API headless IA |
| `nexus/` | `axis-nexus` | API headless de decisiones sensibles |
| `bff/` | `axis-bff` | HTTP BFF para `console/` |
| `console/` | `axis-console` | UI admin/ops |

## Reglas

- `companion` y `nexus` no poseen UI propia como runtime productivo.
- El browser habla con `console`; `console` habla con `bff`; `bff` habla por
  HTTP con `companion` y `nexus`.
- Cada deployable mantiene sus env vars, secrets, DB y pipeline.
- Los imports directos entre internals de servicios quedan prohibidos; la
  comunicación entre servicios es HTTP + contratos compartidos.
- `companion` y `nexus` validan identidad y `org_id` por sí mismos; el BFF no
  es el único boundary de multi-tenancy.
- Releases por componente: `companion-v*`, `nexus-v*`, `bff-v*`,
  `console-v*`.

## Desarrollo local

```bash
make test
docker compose config --services
```

Para levantar todo el stack local:

```bash
test -f .env || cp .env.example .env
docker compose up -d --build
```

Para desarrollo con hot reload de APIs:

```bash
docker compose up -d companion-postgres nexus-postgres
make dev-apis
```

Puertos por defecto:

| Servicio | URL |
|---|---|
| Axis Console | `http://localhost:13000` |
| Axis BFF | `http://localhost:18080` |
| Companion API | `http://localhost:18085` |
| Nexus API | `http://localhost:18084` |

## Deploy DEV

Los deploys Cloud Run viven en `.github/workflows/` y son independientes por
deployable: `deploy-nexus-dev`, `deploy-companion-dev`, `deploy-bff-dev` y
`deploy-console-dev`.

Release, rollback y branch-protection recomendada estan documentados en
`docs/release-rollback.md`.

Variables GitHub comunes:

- `GCP_PROJECT_ID_DEV`, `GCP_REGION`, `WIF_PROVIDER_DEV`,
  `WIF_SERVICE_ACCOUNT_DEV`, `ARTIFACT_REGISTRY`.
- `CLOUDSQL_INSTANCE_DEV` para Nexus y Companion.
- Service accounts: `NEXUS_CLOUD_RUN_SERVICE_ACCOUNT_DEV`,
  `COMPANION_CLOUD_RUN_SERVICE_ACCOUNT_DEV`,
  `AXIS_BFF_CLOUD_RUN_SERVICE_ACCOUNT_DEV`,
  `AXIS_CONSOLE_CLOUD_RUN_SERVICE_ACCOUNT_DEV`.
- URLs entre servicios: `COMPANION_NEXUS_BASE_URL_DEV`,
  `AXIS_NEXUS_BASE_URL_DEV`, `AXIS_COMPANION_BASE_URL_DEV`,
  `AXIS_BFF_BASE_URL_DEV`.

Secrets en Secret Manager:

- Nexus: `nexus-db-password`, `nexus-api-keys`, `nexus-signing-key`,
  `nexus-callback-token`.
- Companion: `companion-db-password`, `companion-api-keys`,
  `companion-nexus-api-key`.
- Shared: `axis-internal-jwt-secret`, usado por BFF, Nexus y Companion.
