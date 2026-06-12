# PROJECT_CONTEXT.md

Contexto durable del proyecto Axis. Este archivo fue creado porque no existia
un `PROJECT_CONTEXT.md` previo en la raiz del repo; no reemplaza contenido
anterior.

## Resumen

Axis es un monorepo de control operativo para IA, decisiones sensibles y
consola admin/ops. Agrupa deployables independientes: vivir en el mismo repo no
implica mismo runtime, misma base de datos, mismos secrets ni mismo ciclo de
deploy.

```text
axis/
+-- companion/   # servicio headless de IA/runtime/tools/memory
+-- nexus/       # servicio headless de policies/approvals/audit
+-- bff/         # backend-for-frontend de la consola operativa
+-- console/     # UI admin/ops de Axis
+-- packages/    # contratos, auth, UI compartida
```

## Deployables y flujo

| Carpeta | Deployable | Rol |
|---|---|---|
| `companion/` | `axis-companion` | API headless IA |
| `nexus/` | `axis-nexus` | API headless de decisiones sensibles |
| `bff/` | `axis-bff` | HTTP BFF para `console/` |
| `console/` | `axis-console` | UI admin/ops |

Flujo productivo:

```text
browser -> console -> bff -> HTTP -> companion
                         \-> HTTP -> nexus
```

Reglas confirmadas:

- `companion` y `nexus` no poseen UI propia como runtime productivo.
- Los imports directos entre internals de servicios estan prohibidos.
- La comunicacion entre servicios es HTTP mas contratos compartidos.
- Cada deployable mantiene sus env vars, secrets, DB y pipeline.
- Releases por componente: `companion-v*`, `nexus-v*`, `bff-v*`,
  `console-v*`.

## Desarrollo local

Comandos principales desde la raiz:

```bash
make test
make qa
docker compose config --services
```

Levantar todo el stack local:

```bash
test -f .env || cp .env.example .env
docker compose up -d --build
```

Hot reload de APIs:

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

Servicios de compose confirmados:

```text
companion-postgres
nexus-postgres
nexus
companion
bff
console
```

## Companion

Companion es el servicio headless central de IA de Axis. Concentra runtime LLM,
agentes, memoria, tools, planificacion, watchers y ejecucion asistida para ser
consumido por productos, gateways y servicios internos.

Responsabilidades:

- Orquestar LLMs, agentes, tools y memoria.
- Preparar evidence y contexto operativo.
- Consultar Nexus antes de acciones sensibles.
- Persistir traces operativas del runtime IA.
- Exponer APIs headless para productos, gateways, BFFs y servicios internos.

Limites:

- No evalua policies.
- No aprueba ni rechaza requests como motor de decision.
- No reimplementa risk engine ni audit fuerte.
- No guarda memoria sin customer org/user/product context cuando aplica.
- No mezcla datos entre customer orgs.
- No posee UI productiva propia.

Identidad de trabajo:

- Companion debe evolucionar como empleado IA agnostico de negocio.
- `org_id` sigue siendo el campo fisico/API publico, pero dentro de Companion
  representa `customer_org_id`: la empresa cliente donde trabaja el empleado IA.
- `human_user_id` identifica al humano asociado cuando existe.
- `companion_principal` identifica la cuenta tecnica del agente; el default es
  `companion.employee_ai`.
- `on_behalf_of` representa delegacion explicita desde la identidad autenticada.
- `tenant` queda como termino historico/de compatibilidad en campos persistidos,
  packages o trazas antiguas; no debe crecer como concepto nuevo.
- Los clientes no administran el runtime global de Companion. Solo Axis/owner
  controla prompts, modelos, autonomia, perfiles y capabilities globales.

Modulos actuales destacados:

- `internal/tasks`: lifecycle de tasks, chat, propuestas a Nexus y ejecucion.
- `internal/agents`: perfiles seedables, autonomia y allowlists de tools.
- `internal/runtime`: LLM orchestration, prompt, tool calling, control plane y
  traces.
- `internal/connectors`: registry de connectors, capabilities, idempotencia y
  evidence.
- `internal/memory`: memoria por scope `task`, `org` y `user`, con TTL y cuota.
- `internal/watchers`: automatizaciones proactivas sobre capabilities de
  producto; los tipos Pymes existentes son compatibilidad traducida al modelo
  generico por `product_surface`/`connector_kind`/operation.
- `internal/nexus_assist`: helpers IA para explicar/proponer sobre Nexus.

Variables principales:

- `COMPANION_API_KEYS`
- `COMPANION_AUTH_ISSUER_URL`
- `COMPANION_AUTH_AUDIENCE`
- `COMPANION_INTERNAL_JWT_*`
- `NEXUS_BASE_URL`
- `NEXUS_API_KEY`
- `COMPANION_LLM_*`

## Nexus

Nexus es el servicio headless para decisiones sensibles. Decide `allow`,
`deny` o `require_approval`, y administra approvals, policies, action types,
delegations, RBAC, audit y evidence packs.

Contrato actual:

- Auth inbound: `X-API-Key` o Bearer JWT/OIDC interno.
- Datos operativos con ownership por org requieren `org_id` no vacio.
- Config compartida explicita: `policies`, `action_types` y `delegations`
  pueden ser globales o por org.
- La escritura de globales requiere `nexus:cross_org`.
- Nexus no incluye runtime LLM, memoria IA ni UI propia.

Modulos actuales destacados:

- `internal/requests`: requests y decisiones.
- `internal/approvals`: aprobaciones y expiracion.
- `internal/policies`: reglas CEL para requests.
- `internal/actiontypes`: catalogo de action types.
- `internal/delegations`: delegaciones entre actores/agentes.
- `internal/rbac`: scopes y permisos.
- `internal/audit`: audit fuerte.
- `internal/evidence`: evidence packs.
- `internal/dashboard`: lecturas agregadas para operacion.
- `internal/learning`: propuestas de aprendizaje.

## BFF y Console

`bff` es el backend-for-frontend de `console`.

Responsabilidades del BFF:

- Validar sesion humana con OIDC (`AXIS_BFF_AUTH_MODE=oidc`) o modo dev local.
- Resolver el `org_id` efectivo. En modo operador/cross-org puede usar
  `X-Axis-Org-ID` si el principal tiene scope `axis:cross_org` o
  `nexus:cross_org`.
- Firmar Bearer JWT interno para `companion` y `nexus` acotado al org efectivo,
  con claims `actor_id`, `actor_type`, `org_id`, `product_surface`,
  `service_principal`, `on_behalf_of`, `scope` y `scp`.
- Exponer `/api/companion/*`, `/api/nexus/*`, `/api/session`,
  `/api/services`, `/healthz` y `/readyz`.

`console` es la UI operativa/admin. El browser no llama directo a
`companion` ni `nexus`; todo acceso operativo pasa por `bff`.

Stack de console:

- React 19, TypeScript, Vite.
- Node `22.12.0` segun `console/.nvmrc`.
- Scripts: `npm run typecheck`, `npm run build`, `npm run dev`.

## Packages

- `packages/contracts`: contratos compartidos entre `companion`, `nexus`,
  `bff` y `console`.
- `packages/auth`: helpers compartidos de identidad interna, scopes, tenants y
  token exchange; `tenant` es nombre historico en algunas primitivas, pero
  Companion debe hablar semanticamente de customer org/work context.
- `packages/ui`: componentes visuales compartidos para `console`, sin logica
  pesada de dominio de Nexus o Companion.

La regla es compartir contratos, no internals de servicios.

## Auth, customer orgs y seguridad

- `companion` y `nexus` validan identidad y `org_id` por si mismos.
- El BFF no es el unico boundary multi-tenant.
- El BFF selecciona org con `X-Axis-Org-ID` solo cuando corresponde y emite un
  JWT interno ya acotado al org seleccionado.
- Companion no debe confiar en headers manuales como fuente canonica cuando hay
  principal autenticado; los headers `X-Org-ID`/`X-User-ID` quedan como
  compatibilidad temporal para dev/tests.
- Cross-org directo en Companion requiere scope explicito `companion:cross_org`.
- El BFF firma JWT interno con `AXIS_INTERNAL_JWT_SECRET`, issuer
  `AXIS_INTERNAL_JWT_ISSUER` y audiences separadas para Companion y Nexus.
- En modo dev, `AXIS_DEV_ORG_ID`, `AXIS_DEV_USER_ID` y `AXIS_DEV_SCOPES`
  configuran la identidad local.
- `companion` y `nexus` aceptan API keys o JWT/OIDC segun configuracion.
- No commitear `.env`; usar `.env.example` como plantilla.
- No registrar API keys, bearer tokens ni payloads sensibles sin redaccion.

## Decisiones de producto confirmadas

- IA vive en Companion.
- Decisiones sensibles viven en Nexus.
- Dominio vertical vive en productos externos.
- Companion es un empleado IA transversal y agnostico de negocio.
- Nexus se mantiene deterministico.
- Companion se mantiene IA asistida.
- Productos como Argos son duenos de su conocimiento de negocio; Axis almacena
  y ejecuta configuracion provista por productos, pero no define conocimiento
  vertical en codigo.
- `nexus-assist` se mantiene enfocado en explicar requests y propuestas de
  aprendizaje de Nexus.

## Deploy DEV

Los deploys Cloud Run viven en `.github/workflows/` y son independientes por
deployable:

- `deploy-nexus-dev`
- `deploy-companion-dev`
- `deploy-bff-dev`
- `deploy-console-dev`

Variables GitHub comunes:

- `GCP_PROJECT_ID_DEV`
- `GCP_REGION`
- `WIF_PROVIDER_DEV`
- `WIF_SERVICE_ACCOUNT_DEV`
- `ARTIFACT_REGISTRY`
- `CLOUDSQL_INSTANCE_DEV` para Nexus y Companion

Service accounts por deployable:

- `NEXUS_CLOUD_RUN_SERVICE_ACCOUNT_DEV`
- `COMPANION_CLOUD_RUN_SERVICE_ACCOUNT_DEV`
- `AXIS_BFF_CLOUD_RUN_SERVICE_ACCOUNT_DEV`
- `AXIS_CONSOLE_CLOUD_RUN_SERVICE_ACCOUNT_DEV`

URLs entre servicios:

- `COMPANION_NEXUS_BASE_URL_DEV`
- `AXIS_NEXUS_BASE_URL_DEV`
- `AXIS_COMPANION_BASE_URL_DEV`
- `AXIS_BFF_BASE_URL_DEV`

Secrets documentados en Secret Manager:

- Nexus: `nexus-db-password`, `nexus-api-keys`, `nexus-signing-key`,
  `nexus-callback-token`.
- Companion: `companion-db-password`, `companion-api-keys`,
  `companion-nexus-api-key`.
- Shared: `axis-internal-jwt-secret`, usado por BFF, Nexus y Companion.

## Posiblemente obsoleto / pendiente de confirmar

Estos puntos existen en cambios locales no trackeados o docs no confirmados por
commit al momento de crear este archivo. No eliminarlos ni tratarlos como
definitivos sin revisar el estado actual del repo:

- `nexus/internal/findings`, migrations `0020_findings` y ADR
  `docs/adr/0001-product-owned-knowledge.md`.
- `companion/internal/assist`, migrations `0018_assist_packs` y contratos de
  assist-packs / assist-runs.
- Workflows de deploy DEV no trackeados en `.github/workflows/`.
- Cambios locales en `Makefile`, `README.md`, Dockerfiles, compose, `go.mod` y
  wire setup/auth de Companion y Nexus.

## Fuentes y cambios de este archivo

Fuentes usadas:

- `README.md`
- `companion/README.md` y `companion/docs/`
- `nexus/README.md` y `nexus/docs/development.md`
- `bff/README.md`
- `console/README.md`
- `packages/README.md`, `packages/auth/README.md`,
  `packages/contracts/README.md`, `packages/ui/README.md`
- `Makefile`
- `docker-compose.yml`
- `.env.example`
- `.github/workflows/`
- `docs/adr/0001-product-owned-knowledge.md`

Cambios:

- Archivo creado porque no existia `PROJECT_CONTEXT.md` en la raiz de Axis.
- No se elimino ni reemplazo contenido previo.
- La informacion de features no trackeadas quedo separada en
  "Posiblemente obsoleto / pendiente de confirmar".
- Actualizado para documentar la identidad de trabajo de Companion:
  `org_id` como customer org, BFF seleccionando org con `X-Axis-Org-ID`,
  `companion.employee_ai` como principal tecnico y `tenant` como termino
  historico/de compatibilidad.
