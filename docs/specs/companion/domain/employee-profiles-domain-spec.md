# Employee Profiles Domain Spec

## Proposito

Este spec define `EmployeeProfile` como perfil tecnico/cognitivo reusable para
Virtual Employees.

Nombre actual en el repo: `AgentProfile`.

Nombre objetivo publico: `EmployeeProfile`.

## EmployeeProfile

Definicion: plantilla tecnica de comportamiento para un employee.

Utilidad: define prompt base, autonomia maxima, capabilities default y
configuracion minima de LLM/memoria.

No representa: puesto de trabajo, employee concreto, permisos reales ni agente.

Tipo: entidad fuerte.

CRUD objetivo: si.

Audiencia: admin/dev avanzado.

Modelo objetivo:

```text
EmployeeProfile
- profile_id: UUID
- profile_key: string
- name: string
- system_prompt: string
- max_autonomy: AutonomyLevel
- default_capability_ids: UUID[]
- memory_policy: MemoryPolicy
- llm_config: LLMConfig
- status: ProfileStatus
```

Enums:

```text
AutonomyLevel: A0, A1, A2, A3, A4, A5
ProfileStatus: draft, active, archived
```

Relaciones:

```text
VirtualEmployee.profile_id -> EmployeeProfile.profile_id
EmployeeProfile.default_capability_ids -> Capability.capability_id[]
Agent.profile_id -> EmployeeProfile.profile_id
```

Estado actual: existe `agent_profiles` con `id uuid`, `profile_id text`,
`system_prompt`, `max_autonomy`, `allowed_tools`, `allowed_capabilities`,
`memory_policy_json` y `llm_config_json`.

Brecha: `profile_id text` actual debe ser `profile_key`; el ID fuerte objetivo
es UUID. La nomenclatura publica debe dejar de decir `AgentProfile` cuando se
configura un employee.

## LLMConfig

Definicion: parametros minimos de modelo LLM.

Utilidad: configura el comportamiento tecnico del perfil.

No representa: provider credential, policy, presupuesto ni routing dinamico.

Tipo: value object embebido.

CRUD objetivo: no propio.

Modelo objetivo:

```text
LLMConfig
- provider: string
- model: string
- temperature: decimal
- max_tokens: integer
```

Reglas:

- `temperature` debe ser decimal entre `0` y `2`.
- `max_tokens` debe ser entero positivo.

Estado actual: vive como `llm_config_json`.

Brecha: documentar y validar la forma; no dejarlo como objeto libre.

## MemoryPolicy

Definicion: regla simple de memoria aplicable al perfil o a una memoria.

Utilidad: declara si el perfil espera memoria y que scopes puede usar.

No representa: Memory ni MemoryEntry.

Tipo: value object embebido.

CRUD objetivo: no propio.

Modelo objetivo:

```text
MemoryPolicy
- enabled_by_default: boolean
- retention_days: integer
- allow_user_memory: boolean
- allow_task_memory: boolean
- allow_tenant_memory: boolean
```

Reglas:

- `retention_days` `0` significa sin expiracion automatica definida por el
  perfil.

Estado actual: vive como `memory_policy_json`.

Brecha: documentar y validar la forma.

## Auditoria Actual Vs Objetivo

| Concepto actual | Problema | Modelo objetivo |
|---|---|---|
| `AgentProfile` | Nombre arrastra Agent al dominio publico. | `EmployeeProfile`. |
| `profile_id text` | Es key semantica. | `profile_id UUID`; `profile_key string`. |
| `allowed_tools` | Expone tools directo en perfil publico. | Preferir `default_capability_ids`. |
| `allowed_capabilities text[]` | Referencia keys textuales. | `default_capability_ids UUID[]`. |
| `memory_policy_json`, `llm_config_json` | Objetos libres. | `MemoryPolicy` y `LLMConfig` documentados. |

