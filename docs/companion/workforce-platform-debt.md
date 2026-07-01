# Workforce, platform y deudas tecnicas

Este documento fija el limite despues de separar `Virployee` de `Agent`.

El backlog operativo y la brecha contra specs viven en
`workforce-implementation-gap.md`.

## Limite de platform

`platform` no conoce Workforce.

No debe tener conceptos como:

- `Virployee`
- `JobRole`
- `Capability`
- `Handoff`
- `VirployeeProfile`

`platform` solo aporta primitivas reutilizables:

- audit/lifecycle generico;
- contratos compartidos;
- helpers de auth, HTTP, errores y DB.

En Axis, los contratos TS publicos de Workforce viven en
`packages/contracts/workforce.ts`.

## Audit comun

Companion usa `companion/internal/audit` como adapter unico para escribir
eventos comunes en `lifecycle_audit`.

`platform/lifecycle/go` define la primitiva generica como `AuditEvent` y
`AuditPort` desde la version `0.3.0`. Axis mantiene el adapter local en
Companion y debe subir `companion/go.mod`/`nexus/go.mod` a esa version cuando el
modulo este taggeado/publicado. No commitear `replace` local para esto.

Resource types publicos:

- `virployee`
- `job_role`
- `virployee_profile`
- `memory`
- `task`
- `watcher`
- `handoff`

Las tablas de audit especificas pueden seguir existiendo como snapshots internos
o versioning durante la transicion. La lectura publica recomendada es:

```text
GET /v1/audit-events
GET /api/audit-events
```

Esto no reemplaza Nexus evidence ni approvals.

## Handoffs publicos

Los handoffs publicos son Virployee -> Virployee.

Endpoints recomendados:

```text
GET    /v1/handoffs
POST   /v1/handoffs
GET    /v1/handoffs/{handoff_id}
PATCH  /v1/handoffs/{handoff_id}

GET    /api/handoffs
POST   /api/handoffs
GET    /api/handoffs/{handoff_id}
PATCH  /api/handoffs/{handoff_id}
```

No usan `from_agent_id` ni `to_agent_id`.

Los handoffs tecnicos de `/v1/agents/handoffs` quedan solo para runtime tecnico
hasta retirarlos.

## VirployeeProfile

El nombre publico correcto es `VirployeeProfile`.

`AgentProfile` solo puede quedar como resto tecnico pendiente de migracion
fisica. No debe aparecer en nuevas superficies publicas de Workforce.

Endpoints recomendados:

```text
/v1/virployee-profiles
/api/virployee-profiles
```

## Deuda que queda

- Renombrar fisicamente `agent_profiles` a `companion_virployee_profiles` y
  retirar el nombre viejo del codigo productivo.
- Publicar/taggear `platform/lifecycle/go v0.3.0` y despues cambiar Axis de
  `lifecycle.ArchiveAudit` a `lifecycle.AuditEvent`.
- Migrar todos los snapshots de audit anteriores a lectura publica por
  `AuditEvent`.
- Emitir audit comun desde todos los updates de Tasks y Watchers cuando esos
  modulos reciban `tenant_id` UUID limpio. Hoy todavia operan principalmente
  con `org_id`, y forzar `tenant_id = org_id` mezclaria dominios.
