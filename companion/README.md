# Companion

Servicio headless de IA transversal del ecosistema. Companion concentra runtime LLM,
agentes, memoria, tools, planificación y ejecución asistida para ser consumido
por productos, gateways y servicios internos. Consume
**Nexus Governance** (proyecto separado) para toda acción sensible que requiera
policy, approval, risk o audit fuerte.

> La DB se llama `companion`; el módulo Go es `github.com/devpablocristo/companion`.

## Boundaries arquitectónicos (regla dura)

- **IA = Companion**, **Gobernanza = Nexus**, sin excepciones.
- Companion **nunca** evalúa policies, nunca decide approve/deny, nunca ejecuta
  approvals. Para cualquier decisión gobernada, llama a Nexus por HTTP via
  `platform/kernels/governance/go/governanceclient`.
- Nexus **nunca** importa código LLM ni depende de un proveedor de IA. Los
  helpers de IA (proposer de policies, contextualizer de approvals) viven en
  `internal/governance_assist/` de este repo y se exponen como secondary
  calls que la consola de Nexus puede consumir.
- El runtime LLM de Companion **no tiene** tools de approve/reject de
  governance — el contract test
  `scripts/quality/check-governance-imports.sh` bloquea el merge si
  alguien reintroduce los packages eliminados de
  `platform/kernels/governance/go/{decision,policy,risk,approval,kernel}` (todos
  movidos a `nexus/governance/internal/`).
- Companion **no posee UI de usuario final ni console propia en runtime**. La
  administración y monitoreo se hacen por APIs, métricas y `../console`, vía
  `../bff`.

## Estructura

```
companion/
├── cmd/api/                 # entry point del backend Go
├── internal/                # tasks, runtime, connectors, memory, watchers, governance_assist
├── wire/                    # DI manual + cliente HTTP a Nexus governance
├── migrations/              # PostgreSQL embebidas
├── scripts/
│   ├── lib/common.sh
│   ├── smoke/run-companion-*.sh
│   ├── dev/ensure-companion-db.sh
│   └── quality/{check-migrations,go-in-env}.sh
├── Dockerfile
├── docker-compose.yml       # companion + companion-postgres
├── go.mod
├── Makefile
└── .env.example
```

## Requisitos

- PostgreSQL (la DB `companion` se crea automáticamente desde el container).
- **Nexus Governance** accesible vía `GOVERNANCE_BASE_URL` y `GOVERNANCE_API_KEY`
  (proyecto separado en `../nexus/`).

## Arranque rápido

Levantá Nexus governance primero (en `../nexus/`):

```bash
cd ../nexus
make up
```

Después companion (este repo):

```bash
test -f .env || cp .env.example .env
make up
```

URLs por defecto (host):

| Servicio       | URL                       |
|----------------|---------------------------|
| Companion API  | `http://localhost:18085`  |
| Nexus Gov API  | `http://localhost:18084`  |

## Variables de entorno principales

Ver `.env.example`.

Convenciones:
- `GOVERNANCE_BASE_URL`, `GOVERNANCE_API_KEY` — apuntan al servicio Nexus governance externo.
- `COMPANION_API_KEYS` (dentro del container) — auth del propio Companion.
  Soporta metadata: `actor`, `org_id`, `scopes`, `service_principal`.
- `COMPANION_AUTH_*` — OIDC/JWKS opcional para gateways o servicios internos
  que llamen a Companion con identidad delegada.
- `COMPANION_INTERNAL_JWT_*` — Bearer JWT interno emitido por Axis BFF.
- `COMPANION_LLM_PROVIDER=vertex`, `COMPANION_LLM_MODEL`,
  `COMPANION_LLM_VERTEX_PROJECT`, `COMPANION_LLM_VERTEX_LOCATION` —
  runtime IA de Companion, exclusivamente Gemini via Vertex AI.
- `COMPANION_GOVERNANCE_SYNC_INTERVAL_SEC` — período del loop que reconcilia
  decisiones de governance con propuestas pendientes.
- `COMPANION_STRICT_GOVERNANCE` — cuando está en `true`, Companion falla
  cerrado para ejecuciones sensibles sin grant Nexus exacto.
- `PYMES_BASE_URL` / `PYMES_API_KEY` — adapter Pymes, opcional.
- `PONTI_BASE_URL` / `PONTI_API_KEY` — adapter Ponti por manifest, opcional.
- `COMPANION_WATCHER_INTERVAL_SEC` — loop proactivo de watchers.
- `COMPANION_WATCHER_SYNC_INTERVAL_SEC` — reconciliación de proposals de watchers
  pendientes en Nexus.

Scopes relevantes:

| Scope | Uso |
|---|---|
| `companion:tasks:read` / `companion:tasks:write` | Tasks y chat |
| `companion:connectors:execute` / `companion:connectors:admin` | Capabilities y ejecución |
| `companion:watchers:read` / `write` / `execute` | Watchers |
| `companion:governance:read` / `admin` | Integración Nexus; runtime solo expone datos Nexus con `admin` |
| `companion:governance-assist:read` / `admin` | Helpers IA sobre Nexus |

## Tests

```bash
make test                    # Go unit
make qa                      # build + vet + test -race
make smoke                   # smoke contra companion + nexus levantados
```

## Documentación

- `ARCHITECTURE.md` — mapa del sistema y flujos.
- `BOUNDARIES.md` — responsabilidades Companion/Nexus/productos/platform.
- `MEMORY.md` — scopes, aislamiento y retención.
- `AGENTS.md` — agent profile mínimo, autonomía y tool allowlist.
- `TOOLS.md` — catálogo de tools y reglas de exposición.
- `NEXUS_INTEGRATION.md` — decisiones, evidence y result reporting.
- `SECURITY.md` — auth, scopes, multi-tenant y prompt injection.
- `TESTING.md` — suites obligatorias.
- `OPERATIONS.md` — runbook local/operativo.
- `openapi.yaml` — contrato HTTP inicial.
