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

## GitHub Flow Deploys

Los deploys Cloud Run viven en `.github/workflows/` y siguen GitHub Flow:

- PR contra `main`: CI y preview automatico.
- Push a `main`: `deploy-stg`.
- Produccion: `deploy-prd` manual con GitHub environment `prd`.

Release, rollback y branch-protection recomendada estan documentados en
`docs/release-rollback.md`.

Readiness multi-producto antes del primer producto real esta documentado en
`docs/axis-ready-for-first-real-product.md`.

STG usa el proyecto `axis-stg-884236` y la instancia Cloud SQL existente
`pymes-dev-352318:us-central1:pymes-dev-db`. No se crean instancias de base de
datos desde los workflows. STG expone `axis-nexus`, `axis-companion`,
`axis-bff` y `axis-console`.

PRD se configura con variables GitHub `*_PRD` y secrets de Secret Manager
propios del entorno productivo.

Secrets en Secret Manager:

- Nexus STG: `axis-nexus-database-url-stg`, `axis-nexus-api-keys-stg`,
  `axis-nexus-signing-key-stg`, `axis-nexus-callback-token-stg`.
- Companion STG: `axis-companion-database-url-stg`,
  `axis-companion-api-keys-stg`, `axis-companion-nexus-api-key-stg`.
- Shared STG: `axis-internal-jwt-secret-stg`, usado por BFF, Nexus y Companion.
