# Job Roles Domain Spec

## Proposito

Este spec define `JobRole` como puesto de trabajo. No lo confunde con IAM Role,
PermissionBundle ni permisos reales.

## JobRole

Definicion: puesto de trabajo dentro de un tenant.

Utilidad: define la funcion que puede ocupar un `Virployee`.

No representa: permisos reales, perfil tecnico, employee concreto ni agente.

Tipo: entidad fuerte.

CRUD objetivo: si.

Audiencia: publica admin.

Modelo objetivo:

```text
JobRole
- job_role_id: UUID
- tenant_id: UUID
- name: string
- slug: string
- mission: string
- responsibilities: Responsibility[]
- success_criteria: SuccessCriterion[]
- recommended_capability_ids: UUID[]
- default_autonomy: AutonomyLevel
- status: JobRoleStatus
```

Enums:

```text
JobRoleStatus: active, archived, trash
AutonomyLevel: A0, A1, A2, A3, A4, A5
```

Relaciones:

```text
JobRole.tenant_id -> Tenant.tenant_id
JobRole.recommended_capability_ids -> Capability.capability_id[]
Virployee.job_role_id -> JobRole.job_role_id
```

Estado actual: existe `companion_job_roles` con `id uuid`,
`job_role_id text`, `org_id`, `product_surface`, `metadata_json`, defaults y
audit.

Brecha: `job_role_id` objetivo debe ser UUID. El campo textual debe ser `slug`
o una key, no el ID fuerte. `org_id + product_surface` debe reemplazarse por
`tenant_id` en el modelo objetivo.

## Responsibility

Definicion: obligacion estable de un `JobRole`.

Utilidad: explica que debe cumplir el puesto.

No representa: task, KPI, policy ni permiso.

Tipo: value object embebido.

CRUD objetivo: no en v1.

Audiencia: publica dentro de `JobRole`.

Modelo objetivo:

```text
Responsibility
- title: string
- description: string
- expected_outcome: string
- priority: integer
```

Reglas:

- `priority` es entero positivo.
- Vive embebida en `JobRole`.
- No tiene ID propio ni lifecycle propio.

Estado actual: existe como struct Go y JSONB dentro de `JobRole`.

Brecha: la persistencia actual como JSONB es aceptable mientras la forma este
validada y documentada; no crear CRUD propio hasta que tenga lifecycle real.

## SuccessCriterion

Definicion: criterio usado para evaluar si el puesto cumple su funcion.

Utilidad: permite expresar resultados esperados sin crear KPI formal todavia.

No representa: metrica calculada, SLA ni approval.

Tipo: value object embebido.

CRUD objetivo: no en v1.

Audiencia: publica dentro de `JobRole`.

Modelo objetivo:

```text
SuccessCriterion
- title: string
- description: string
- target_value: string
- priority: integer
```

Estado actual: existe como `success_criteria text[]`.

Brecha: el modelo objetivo requiere estructura, no solo strings, si se necesita
evaluacion o UI rica.

## Auditoria Actual Vs Objetivo

| Concepto actual | Problema | Modelo objetivo |
|---|---|---|
| `job_role_id text` | Mezcla ID fuerte con slug/key. | `job_role_id UUID`; `slug string`. |
| `org_id + product_surface` | Tenant implicito. | `tenant_id UUID`. |
| `recommended_capabilities text[]` | Referencia keys, no capabilities fuertes. | `recommended_capability_ids UUID[]`. |
| `default_permission_bundle_id` | Puede parecer permiso real. | Fuera del core de JobRole v1 o documentado en specs de permisos. |
| `metadata_json` | Campo generico sin forma de dominio. | Value objects explicitos o eliminado del core. |
