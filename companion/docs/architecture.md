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
| `internal/runtime` | LLM orchestration, prompt, tool calling, control plane y traces |
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
- Memory: upsert/find/get/delete por scope; runtime solo recuerda si tiene
  identidad válida.
- Watchers: consultan capabilities read del producto, crean proposals,
  consultan Nexus y ejecutan side effects vía connectors.

## Persistencia

Postgres guarda tasks, messages, actions, artifacts, nexus sync state,
execution plans/state, watchers/proposals, memory entries, connectors/executions
y run traces. `companion_run_traces` incluye `prompt_version` y `model` para
auditar runtime IA.

## Runtime IA

El runtime usa providers de `platform/kernels/ai/go`. El prompt tiene versión
`companion.system.v1`. El control plane construye una `IdentityChain`, un
`AgentRoute` y un `AgentProfile` efectivo con allowlist de tools. El LLM solo
recibe schemas autorizados para tenant/scopes presentes.

## UI operativa

Companion no despliega UI propia. La observabilidad y administración se exponen
por APIs, logs y métricas; la UI unificada vive fuera del servicio en
`../console` y accede vía `../bff`.

## Configuración local

El servicio requiere `DATABASE_URL`, `COMPANION_API_KEYS`,
`NEXUS_BASE_URL` y `NEXUS_API_KEY`. Pymes, Ponti, OIDC y LLM real son
opcionales. Ver `../../.env.example` en la raíz de Axis y `docs/operations.md`.
