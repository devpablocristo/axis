# Virtual Employees Domain Spec

## Proposito

Este spec define el modelo objetivo de dominio para `VirtualEmployee` en
Companion.

Describe el modelo objetivo. La implementacion actual ya separa
`VirtualEmployee` de `Agent`, pero todavia conserva brechas de transicion en
tenancy, perfiles, capabilities y runtime.

## Definicion

Un `VirtualEmployee` es un trabajador digital persistente con identidad propia,
dentro de un tenant, supervisado por un humano, que ocupa un puesto y recibe
trabajo usando perfiles, capabilities y memoria.

## Utilidad

- Es la entidad principal de Workforce.
- Es lo que el usuario crea, lista, asigna y opera.
- Permite separar el lenguaje de producto del runtime tecnico.

## Que No Representa

`VirtualEmployee` no representa:

- un `Agent`;
- una flota de agents;
- un profile tecnico;
- un puesto de trabajo;
- una tool concreta;
- un permiso real;
- un audit log;
- una task.

## Tipo, CRUD Y Audiencia

Tipo: entidad fuerte.

CRUD objetivo: si.

Audiencia: publica para usuarios operativos y admins.

## Modelo Objetivo

```text
VirtualEmployee
- employee_id: UUID
- tenant_id: UUID
- name: string
- supervisor_user_id: UUID
- status: EmployeeStatus
- job_role_id: UUID
- profile_id: UUID
- autonomy: AutonomyLevel
- capability_ids: UUID[]
- memory_id: UUID | null
```

Este core representa:

- identidad: `employee_id`, `tenant_id`, `name`, `supervisor_user_id`,
  `status`;
- puesto: `job_role_id`;
- comportamiento tecnico: `profile_id`, `autonomy`;
- habilidades: `capability_ids`;
- memoria: `memory_id`.

## Enums

```text
EmployeeStatus: draft, active, disabled, suspended, archived, trashed, error
AutonomyLevel: A0, A1, A2, A3, A4, A5
```

## Relaciones Por ID

```text
tenant_id -> Tenant.tenant_id
supervisor_user_id -> User.user_id
job_role_id -> JobRole.job_role_id
profile_id -> EmployeeProfile.profile_id
capability_ids -> Capability.capability_id[]
memory_id -> Memory.memory_id | null
```

`VirtualEmployee` no contiene el detalle de esas entidades. Solo conserva la
referencia necesaria para componer el trabajador digital.

## Campos Excluidos Del Core

Estos campos no pertenecen al core objetivo de `VirtualEmployee`:

```text
agent_id
org_id
product_surface
tenant label
owner_user_id
description
job_title
mission
responsibilities
memory_enabled
memory_scope_id
allowed_tools
metadata
source_*
origin_kind
lifecycle_status
review_status
validation_status
created_by
created_at
updated_at
archived_at
trashed_at
version
```

## Reglas De Modelo

`tenant_id` reemplaza `org_id + product_surface` como referencia principal del
employee. `org_id` y `product_surface` pertenecen al `Tenant`.

`supervisor_user_id` reemplaza `owner_user_id`. El supervisor es el humano
responsable del employee; no es necesariamente el creador ni un admin IAM.

`job_role_id` reemplaza duplicar `job_title`, `mission` y
`responsibilities`. Esos campos pertenecen a `JobRole`.

`profile_id` apunta a `EmployeeProfile`. El nombre actual `AgentProfile` es
implementacion/naming historico.

`memory_id` reemplaza `memory_enabled` y `memory_scope_id`. Si el employee no
usa memoria persistente, `memory_id` es `null`.

`capability_ids` referencia el catalogo de `Capability`. No crea capabilities
y no reemplaza el registry.

`allowed_tools` no pertenece al core. Las tools son configuracion tecnica
avanzada; el modelo de dominio debe preferir capabilities.

Tasks, watchers y handoffs no viven dentro del employee. Esas entidades apuntan
al employee:

```text
Task.assignee_employee_id
Watcher.assignee_employee_id
Handoff.from_employee_id
Handoff.to_employee_id
```

Audit no vive dentro del core:

```text
AuditEvent.resource_type = "virtual_employee"
AuditEvent.resource_id = VirtualEmployee.employee_id
```

## Estado Actual En El Repo

V1 actual:

```text
VirtualEmployee -> companion_virtual_employees row
employee_id -> UUID
tenant_id -> UUID recibido desde BFF/Console
job_role_id -> UUID
supervisor_user_id -> UUID
profile_id -> UUID/string segun transicion de EmployeeProfile
capability_ids -> UUID[] segun transicion del catalogo
memory_id -> UUID | null
```

## Brecha Actual Vs Objetivo

| Concepto actual | Problema | Modelo objetivo |
|---|---|---|
| `org_id + product_surface` en servicios existentes | Tenant implicito en contratos antiguos. | `tenant_id UUID`. |
| capabilities historicas por key textual | Keys no son IDs fuertes. | `capability_ids UUID[]`. |
| memory historica por scope tecnico | Scope no es contenedor de memoria. | `memory_id UUID | null`. |
| `AgentProfile` publico | Confunde Employee con Agent. | `EmployeeProfile` publico. |

## Fuera De Alcance De Este Spec

Este spec no define:

- migraciones;
- endpoints;
- BFF;
- Console;
- cambios de Runtime;
- eliminacion tecnica de Agents;
- reglas de Nexus, policies o approvals.
