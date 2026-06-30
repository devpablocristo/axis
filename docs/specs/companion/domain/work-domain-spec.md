# Work Domain Spec

## Proposito

Este spec define entidades de trabajo que se relacionan con
`VirtualEmployee`. No componen al employee.

## Task

Definicion: trabajo concreto asignable.

Utilidad: representa una unidad operativa que puede ser creada, asignada,
ejecutada y cerrada.

No representa: JobRole, watcher, job tecnico ni conversation.

Tipo: entidad fuerte.

CRUD objetivo: si.

Audiencia: publica/operativa.

Modelo objetivo:

```text
Task
- task_id: UUID
- tenant_id: UUID
- assignee_employee_id: UUID | null
- title: string
- description: string
- status: TaskStatus
```

Enums:

```text
TaskStatus: open, assigned, running, blocked, done, cancelled
```

Relaciones:

```text
Task.tenant_id -> Tenant.tenant_id
Task.assignee_employee_id -> VirtualEmployee.employee_id | null
```

Estado actual: existen `companion_tasks` y entidades de task con org/product.

Brecha: deben poder apuntar a `employee_id` objetivo en vez de agent o solo
org/product.

## Watcher

Definicion: observador proactivo que detecta condiciones y puede crear trabajo.

Utilidad: monitorear eventos, schedules o capabilities.

No representa: task, employee, policy ni connector.

Tipo: entidad fuerte.

CRUD objetivo: si.

Audiencia: admin avanzado.

Modelo objetivo:

```text
Watcher
- watcher_id: UUID
- tenant_id: UUID
- assignee_employee_id: UUID | null
- name: string
- trigger_kind: WatcherTriggerKind
- status: WatcherStatus
```

Enums:

```text
WatcherTriggerKind: schedule, event, capability
WatcherStatus: active, paused, archived
```

Relaciones:

```text
Watcher.tenant_id -> Tenant.tenant_id
Watcher.assignee_employee_id -> VirtualEmployee.employee_id | null
```

Estado actual: existen `companion_watchers` con org/product y watcher types.

Brecha: el objetivo relaciona watchers con employees por ID cuando hay
responsable operativo.

## Handoff

Definicion: transferencia de trabajo o contexto entre responsables.

Utilidad: mover ownership operativo sin perder razon y estado.

No representa: approval, permission, task ni agent tecnico.

Tipo: entidad fuerte.

CRUD objetivo: si.

Audiencia: operativa avanzada.

Modelo objetivo:

```text
Handoff
- handoff_id: UUID
- tenant_id: UUID
- from_employee_id: UUID | null
- to_employee_id: UUID | null
- reason: string
- status: HandoffStatus
```

Enums:

```text
HandoffStatus: pending, accepted, rejected, cancelled
```

Relaciones:

```text
Handoff.tenant_id -> Tenant.tenant_id
Handoff.from_employee_id -> VirtualEmployee.employee_id | null
Handoff.to_employee_id -> VirtualEmployee.employee_id | null
```

Estado actual: `companion_agent_handoffs` usa agents.

Brecha: handoff de dominio debe hablar de employees. Handoff tecnico entre
agents puede quedar interno.

## Auditoria Actual Vs Objetivo

| Concepto actual | Problema | Modelo objetivo |
|---|---|---|
| Tasks con org/product | No apuntan claramente a employee. | `assignee_employee_id`. |
| Watchers por org/product | No expresan responsable employee. | `assignee_employee_id`. |
| Agent handoffs | Mezclan runtime Agent con dominio operativo. | Employee handoffs publicos; agent handoffs internos. |

