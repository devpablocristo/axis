# Workforce Domain Spec

## Proposito

Este spec define el mapa objetivo del dominio Workforce en Companion. No
describe la implementacion actual ni exige que las tablas actuales ya coincidan.

La regla central es:

```text
Virployee != Agent
```

`Virployee` es el trabajador digital persistente. `Agent` es un ejecutor
tecnico cuando el runtime necesita uno.

## Mapa Conceptual Objetivo

```text
Organization
+-- Tenant
    +-- User
    +-- JobRole
    +-- VirployeeProfile
    +-- Capability
    +-- Tool
    +-- Memory
    +-- Virployee
    +-- Task
    +-- Watcher
    +-- Handoff
```

Relaciones principales:

```text
Virployee.tenant_id -> Tenant.tenant_id
Virployee.supervisor_user_id -> User.user_id
Virployee.job_role_id -> JobRole.job_role_id
Virployee.profile_id -> VirployeeProfile.profile_id
Virployee.capability_ids -> Capability.capability_id[]
Virployee.memory_id -> Memory.memory_id | null
Agent.virployee_id -> Virployee.virployee_id | null
Task.assignee_virployee_id -> Virployee.virployee_id | null
Watcher.assignee_virployee_id -> Virployee.virployee_id | null
Handoff.from_virployee_id -> Virployee.virployee_id | null
Handoff.to_virployee_id -> Virployee.virployee_id | null
```

## Entidades Fuertes

| Entidad | Definicion | CRUD objetivo | Publica/Interna |
|---|---|---:|---|
| `Organization` | Cuenta organizacional cliente. | Si | Publica admin |
| `Product` | Producto/superficie conectable a Axis. | Si | Publica admin |
| `Tenant` | Instancia de trabajo `Organization x Product`. | Si | Publica admin |
| `User` | Humano que opera o supervisa. | Si | Publica admin |
| `Virployee` | Trabajador digital persistente. | Si | Publica |
| `JobRole` | Puesto de trabajo que puede ocupar un employee. | Si | Publica admin |
| `VirployeeProfile` | Perfil tecnico/cognitivo reusable. | Si | Admin/dev |
| `Capability` | Habilidad reusable por contrato. | Si | Admin/dev |
| `Tool` | Funcion tecnica invocable. | Si tecnico | Interna/dev |
| `Memory` | Contenedor de memoria persistente. | Si | Operativa/admin |
| `Agent` | Ejecutor tecnico del runtime. | Si tecnico | Interna |
| `Task` | Trabajo concreto asignable. | Si | Publica operativa |
| `Watcher` | Observador proactivo. | Si | Admin avanzado |
| `Handoff` | Transferencia de trabajo/contexto. | Si | Operativa avanzada |
| `AuditEvent` | Evento de historial. | No CRUD normal | Interna/admin |

## Value Objects

| Value object | Vive dentro de | CRUD propio | Campos primitivos |
|---|---|---:|---|
| `Responsibility` | `JobRole` | No | `title`, `description`, `expected_outcome`, `priority` |
| `SuccessCriterion` | `JobRole` | No | `title`, `description`, `target_value`, `priority` |
| `LLMConfig` | `VirployeeProfile` | No | `provider`, `model`, `temperature`, `max_tokens` |
| `MemoryPolicy` | `VirployeeProfile`, `Memory` | No | booleans y `retention_days` |

## Enums Compartidos

```text
OrganizationStatus: active, suspended, archived
ProductStatus: active, disabled, archived
TenantStatus: active, suspended, archived
UserStatus: invited, active, disabled, archived
VirployeeStatus: draft, active, disabled, suspended, archived, trashed, error
AutonomyLevel: A0, A1, A2, A3, A4, A5
JobRoleStatus: active, archived, trash
ProfileStatus: draft, active, archived
CapabilityStatus: draft, active, deprecated, blocked
ToolStatus: active, disabled, deprecated
MemoryStatus: active, disabled, archived
MemoryEntryStatus: active, superseded, conflict, rejected, forgotten
AgentStatus: idle, running, disabled, error
TaskStatus: open, assigned, running, blocked, done, cancelled
WatcherStatus: active, paused, archived
HandoffStatus: pending, accepted, rejected, cancelled
RiskClass: low, medium, high, critical
CapabilityMode: read, write, execute
WatcherTriggerKind: schedule, event, capability
```

## Reglas De Diseño

- `Virployee` no contiene datos duplicados de entidades fuertes.
- Toda entidad fuerte se referencia por UUID.
- Keys humanas viven como `*_key` o `slug`, nunca como `*_id` objetivo.
- `Agent` no es alias de `Virployee`.
- `Tool` no compone `Virployee` directamente; el employee usa
  `Capability`.
- Audit e historial viven fuera del core de cada entidad.
- Campos flexibles deben convertirse en value objects documentados.

## Auditoria Actual Vs Objetivo

| Concepto actual | Problema | Modelo objetivo |
|---|---|---|
| `companion_agents` | Storage tecnico de Agent con campos historicos de runtime. | `Agent` tecnico separado de `Virployee`. |
| `agent_profiles` | Nombre publico habla de Agent, no Employee. | `VirployeeProfile` publico; `AgentProfile` queda implementacion actual. |
| `capability_id` text | Es una key semantica, no un ID fuerte. | `capability_id` UUID y `capability_key` string. |
| `job_role_id` text | Es ID textual/slug operativo. | `job_role_id` UUID y `slug` string. |
| `memory_scope_id` | Es scope tecnico, no memoria del employee. | `memory_id` UUID hacia `Memory`. |
| `org_id + product_surface` | Implementacion actual del tenant. | `tenant_id` UUID como referencia de dominio. |
