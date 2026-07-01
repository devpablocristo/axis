# Audit Domain Spec

## Proposito

Este spec define audit como historial separado del core de las entidades.

Regla:

```text
Audit no vive dentro de Virployee.
```

## AuditEvent

Definicion: evento inmutable que registra un cambio o accion sobre un recurso.

Utilidad: explicar quien hizo que, sobre que recurso y cuando.

No representa: estado actual del recurso, policy, approval ni evidence
completa.

Tipo: entidad fuerte de historial.

CRUD objetivo: append-only; no update/delete normal.

Audiencia: admin/interna.

Modelo objetivo:

```text
AuditEvent
- audit_event_id: UUID
- tenant_id: UUID
- actor_user_id: UUID | null
- resource_type: string
- resource_id: UUID
- action: string
- occurred_at: datetime
```

Relaciones:

```text
AuditEvent.tenant_id -> Tenant.tenant_id
AuditEvent.actor_user_id -> User.user_id | null
AuditEvent.resource_id -> ID del recurso indicado por resource_type
```

Reglas:

- `resource_type` debe usar nombres de dominio: `virployee`,
  `job_role`, `virployee_profile`, `capability`, `memory`, `task`,
  `watcher`, `handoff`.
- `action` debe ser verbo/evento estable: `created`, `updated`,
  `archived`, `restored`, `status_changed`.
- `created_at`, `updated_at`, `archived_at`, `trashed_at` y `version` no son
  campos core de `Virployee`; son historial o implementacion tecnica.

Estado actual: existe `lifecycle_audit` como tabla comun append-only para
eventos de lifecycle y existen tablas de audit/snapshot separadas como
`companion_agent_audit`, `companion_job_role_audit`,
`companion_virployee_audit`, `companion_memory_audit` y
`companion_memory_container_audit`.

Brecha: el naming y shape de audit deben alinearse con entidades de dominio,
no con aliases tecnicos.

## Auditoria Actual Vs Objetivo

| Concepto actual | Problema | Modelo objetivo |
|---|---|---|
| `created_at` en core | Campo tecnico mezclado con dominio. | Audit/event log separado. |
| `version` en entidades | Puede ser tecnico de concurrencia/historial. | Mantener fuera del core de dominio. |
| `companion_agent_audit` | Audita Agent tecnico. | Solo usarlo para Agent; Employee usa `virployee`. |
| Audits dispersos | Shapes distintos por modulo. | `lifecycle_audit` como `AuditEvent` comun y snapshots especificos cuando hagan falta. |
