# Job Roles

Un `JobRole` es un puesto de trabajo dentro de una customer org y un product
surface. Define que funcion debe cumplir un Virtual Employee.

En Axis:

```text
tenant = org_id + product_surface
```

## Concepto

`JobRole` representa el puesto. `VirtualEmployee` representa el trabajador
digital que ocupa ese puesto. `Agent / Agent Fleet` sigue siendo la
implementacion interna v1 del Virtual Employee.

`JobRole` no es un IAM Role, Account Role ni PermissionBundle. Puede sugerir
defaults, pero no autoriza acciones directamente.

## Modelo V1

Campos principales:

- `job_role_id`
- `org_id`
- `product_surface`
- `name`
- `slug`
- `description`
- `mission`
- `responsibilities`
- `recommended_capabilities`
- `default_autonomy_level`
- `default_permission_bundle_id`
- `success_criteria`
- `default_sla_policy`
- `default_memory_policy`
- `status`
- `metadata`
- `created_by`
- `created_at`
- `updated_at`
- `archived_at`
- `version`

`Responsibility` vive embebida dentro de `JobRole`:

- `title`
- `description`
- `expected_outcome`
- `priority`

No existe CRUD propio de responsibilities en v1.

## APIs

Companion expone:

```text
GET  /v1/job-roles
GET  /v1/job-roles/{job_role_id}
PUT  /v1/job-roles/{job_role_id}
POST /v1/job-roles/{job_role_id}/archive
POST /v1/job-roles/{job_role_id}/restore
GET  /v1/job-roles/{job_role_id}/versions
```

BFF expone la superficie para Console:

```text
GET  /api/job-roles
GET  /api/job-roles/{job_role_id}
PUT  /api/job-roles/{job_role_id}
POST /api/job-roles/{job_role_id}/archive
POST /api/job-roles/{job_role_id}/restore
GET  /api/job-roles/{job_role_id}/versions
```

No hay delete fisico en v1. El lifecycle soportado es `active` y `archived`.

## Relacion Con Virtual Employees

En v1, un Virtual Employee puede referenciar un JobRole mediante:

```text
metadata.job_role_id
```

Esto evita migrar `companion_agents` y no cambia Runtime. La relacion es una
referencia debil v1; no hay FK fuerte desde Agent Fleet.

Al crear o editar un Virtual Employee, Console puede usar el JobRole para
prellenar defaults seguros como puesto, mision, responsabilidades,
capabilities recomendadas y autonomia. Esos valores siguen siendo editables.

## Defaults No Son Permisos

`recommended_capabilities` son recomendaciones para configurar el Virtual
Employee. No habilitan capabilities por si solas.

`default_permission_bundle_id` es una referencia informativa/default. No otorga
permisos, no reemplaza IAM Role y no toma decisiones de autorizacion.

`default_sla_policy` y `default_memory_policy` son datos de configuracion v1.
No cambian Runtime ni Memory automaticamente.

## Fuera De V1

Todavia no existe:

- CRUD de Responsibility.
- Role CRUD.
- Department como entidad.
- KPIs propios.
- SLA avanzado.
- Permission enforcement desde JobRole.
- Multi-role employee.
- Multi-agent employee.
- FK fuerte con Agent Fleet.
- Cambio de Runtime.
- Cambio de Capabilities, Tools, Jobs, Watchers o Memory.
