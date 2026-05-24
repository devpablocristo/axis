# Companion Architecture

Companion es el servicio headless central de IA del ecosistema: el empleado IA.
Su responsabilidad es conversar, razonar, recordar, planificar, usar tools y
ejecutar acciones asistidas para productos, gateways y servicios internos.
Nexus decide decisiones sensibles; los productos exponen capacidades de dominio.

## Módulos actuales

| Módulo | Responsabilidad |
|---|---|
| `cmd/api` | Bootstrap HTTP, config, migraciones, middleware y shutdown |
| `wire` | Composición de dependencias, auth, clients y loops |
| `internal/tasks` | Lifecycle de tasks, chat, propuestas a Nexus y ejecución |
| `internal/agents` | Perfiles seedables, autonomy y allowlists de tools |
| `internal/capabilities` | Manifests versionados, validación estricta y registry canónico |
| `internal/jobs` | Queue durable, workers, leases, retries y DLQ |
| `internal/runtime` | LLM orchestration, prompt, tool calling, control plane, observability y traces |
| `internal/connectors` | Registry de connectors, capabilities, idempotencia y evidence |
| `internal/memory` | Memoria por scope `task/org/user` con TTL y cuota |
| `internal/watchers` | Automatizaciones proactivas sobre capabilities de producto |
| `internal/nexus_assist` | Helpers IA para explicar/proponer sobre Nexus |
| `migrations` | Esquema Postgres |

## Flujos

- Chat: `/v1/chat` crea o reutiliza task, persiste mensaje, llama al runtime,
  ejecuta tools permitidas y guarda respuesta/traces.
- Task nexus: task -> propose -> Nexus `SubmitRequest` -> sync -> estado
  Companion.
- Execution: execution plan -> validación de nexus -> connector capability
  -> evidence/result -> task verification.
- Capability registry: connector/product manifest -> validación
  `capability_manifest.v1` -> runtime tool schema + action binding Nexus +
  planner metadata.
- Org control plane: `GET/PUT /v1/runtime/policy` administra límites
  versionados por customer org; el runtime cruza esa configuración con
  perfiles, models, tools y capability manifests antes de actuar.
- Memory: upsert/find/get/delete por scope; runtime solo recuerda si tiene
  identidad válida.
- Watchers: consultan capabilities read del producto, crean proposals,
  consultan Nexus y ejecutan side effects vía connectors.
- Jobs: loops periódicos encolan `watcher.run` y
  `watcher.proposals.sync`; workers toman leases, ejecutan handlers, registran
  evidence, reintentan con backoff o mandan a DLQ.
- Observability: cada run registra eventos redacted de start, LLM request,
  guardrails, tool calls y completion; `run replay` cruza trace persistido y
  ledger de eventos.

## Persistencia

Postgres guarda tasks, messages, actions, artifacts, nexus sync state,
execution plans/state, watchers/proposals, memory entries, connectors/executions
y run traces. `companion_jobs` y `companion_job_events` guardan ejecución
durable de trabajos operativos. `companion_observability_events` guarda el
ledger redacted para replay. `companion_run_traces` incluye `prompt_version` y
`model` para auditar runtime IA.

## Runtime IA

El runtime usa providers de `platform/kernels/ai/go`. El prompt tiene versión
`companion.system.v1`. El control plane construye una `IdentityChain`, un
`AgentRoute` y un `AgentProfile` efectivo con allowlist de tools. El LLM solo
recibe schemas autorizados para la customer org/scopes presentes.

La política runtime se versiona con `settings_version` y `control_plane_json`.
Cada update queda registrado en `companion_runtime_policy_audit`. La
configuración por organización puede limitar profiles, agents, tools,
capabilities, connectors, models, autonomy, budgets, retention, memoria,
observabilidad, kill switches y riesgo máximo. Las actions críticas siguen
dependiendo de Nexus; Companion solo reduce o bloquea superficie de ejecución
cuando la organización no autoriza una capability.

## UI operativa

Companion no despliega UI propia. La observabilidad y administración se exponen
por APIs, logs y métricas; la UI unificada vive fuera del servicio en
`../console` y accede vía `../bff`.

## Configuración local

El servicio requiere `DATABASE_URL`, `COMPANION_API_KEYS`,
`NEXUS_BASE_URL` y `NEXUS_API_KEY`. Pymes, Ponti, OIDC y LLM real son
opcionales. Ver `../../.env.example` en la raíz de Axis y `docs/operations.md`.
