# Workforce Implementation Gap

Este documento muestra la diferencia entre el modelo objetivo de Workforce y la
implementacion actual.

Regla:

```text
Spec = modelo objetivo
Codigo = implementacion actual
Gap = deuda real
```

No es un spec nuevo. Es el reporte operativo para decidir que PR sigue.

## Fuentes

- `docs/specs/companion/domain/workforce-domain-spec.md`
- `docs/specs/companion/domain/virtual-employees-domain-spec.md`
- `docs/specs/companion/domain/job-roles-domain-spec.md`
- `docs/specs/companion/domain/employee-profiles-domain-spec.md`
- `docs/specs/companion/domain/capabilities-and-tools-domain-spec.md`
- `docs/specs/companion/domain/memory-domain-spec.md`
- `docs/specs/companion/domain/work-domain-spec.md`
- `docs/specs/companion/domain/audit-domain-spec.md`
- `docs/companion/workforce-platform-debt.md`

## Estado General

| Area | Estado | Lectura corta |
|---|---|---|
| `VirtualEmployee` | Parcial alto | Ya existe como entidad propia, pero todavia convive con transicion de tenancy/perfiles/capabilities. |
| `JobRole` | Parcial alto | Existe y opera, pero aun conserva campos transitorios del primer modelo. |
| `EmployeeProfile` | Parcial medio | API publica limpia, storage fisico todavia viene de `agent_profiles`. |
| `Capability` / `Tool` | Parcial medio | Hay catalogo y API, pero falta cerrar UUID/key como contrato final en todo el stack. |
| `Memory` | Parcial alto | Existe como contenedor nuevo, falta integracion completa con todos los flujos antiguos. |
| `Handoff` | Parcial alto | Handoffs publicos Employee -> Employee existen; los handoffs tecnicos de Agents quedan pendientes de retiro. |
| `Task` | Parcial bajo/medio | Empieza a aceptar `assignee_employee_id`, pero sigue centrado en `org_id`. |
| `Watcher` | Parcial bajo/medio | Expone `assignee_employee_id`, pero sigue centrado en `org_id` y config embebida. |
| `AuditEvent` | Parcial medio | Adapter comun existe, pero Platform/Axis todavia no estan totalmente alineados. |
| `Tenant` | Parcial medio | BFF/Control Plane tienen tenant UUID; Companion todavia conserva `org_id + product_surface` en varios modulos. |
| `Agent` | Correcto como tecnico | Debe quedarse solo como runtime tecnico, no como dominio publico de Employee. |

## Gaps Priorizados

| Prioridad | Gap | Modelo objetivo | Estado actual | Bloqueado por | Siguiente PR recomendado |
|---:|---|---|---|---|---|
| P0 | Versionar Platform lifecycle | Axis debe usar `lifecycle.AuditEvent`. | Platform local ya tiene `AuditEvent`, Axis aun depende de `lifecycle/go v0.2.0`. | Falta tag/publicacion `platform/lifecycle/go v0.3.0`. | Taggear/publicar Platform `v0.3.0`. |
| P0 | Cambiar `ArchiveAudit` a `AuditEvent` | Audit generico no debe llamarse archive. | Axis aun referencia `lifecycle.ArchiveAudit` en Companion/Nexus. | Depende de Platform `v0.3.0` consumible. | Bump de `go.mod` y rename de tipos. |
| P1 | Rename fisico de perfiles | `EmployeeProfile` debe ser entidad publica y storage claro. | API publica es `employee-profiles`, pero tabla/paquete interno siguen `agent_profiles`/`agentprofiles`. | Migracion DB y ajuste runtime profile resolver. | Migrar `agent_profiles` -> `companion_employee_profiles`. |
| P1 | Tenant limpio en Tasks/Watchers | Work domain debe usar `tenant_id UUID`. | Tasks/Watchers siguen trabajando principalmente con `org_id`. | Definir migracion de tenancy por modulo. | Agregar `tenant_id` a Tasks/Watchers sin inventar `tenant_id = org_id`. |
| P1 | Audit comun completo | Cada cambio importante emite `AuditEvent`. | VirtualEmployee/JobRole/Memory/Handoff tienen audit comun; Tasks/Watchers no completo. | Depende de `tenant_id` limpio en esos modulos. | Emitir audit comun para Task/Watcher create/update/status. |
| P2 | Capability IDs fuertes en todos lados | Employees/JobRoles/Profile referencian `capability_id UUID`. | Conviven UUIDs nuevos con keys anteriores. | Catalogo y migracion de referencias existentes. | Normalizar referencias a capabilities por UUID. |
| P2 | JobRole contrato final | `job_role_id UUID`, value objects estructurados, sin campos libres innecesarios. | JobRole existe, pero conserva algunos campos transitorios. | Decidir migracion de datos dev. | Ajustar contrato a spec final y eliminar campos transitorios. |
| P2 | Memory integrada en flujos anteriores | Employee apunta a `memory_id`; runtime usa contenedor. | Memory nueva existe; algunos flujos anteriores aun hablan de scopes/memoria tecnica. | Revisar runtime/memory anterior. | Conectar runtime y flujos anteriores al contenedor `Memory`. |
| P3 | Handoffs tecnicos de Agents a retirar | Publico debe usar Employee -> Employee. | `/v1/handoffs` publico existe; `/v1/agents/handoffs` queda tecnico. | Migrar consumidores tecnicos o confirmar que no existen. | Retirar endpoint tecnico cuando no tenga consumidores. |
| P3 | Limpieza de docs historicas | Docs publicas deben hablar de Workforce limpio. | Docs principales limpias; ADRs/migraciones pueden conservar historia del proyecto. | Separar docs historicas de producto. | Revisar docs no publicas y marcar historia/tecnico. |

## Detalle Por Area

### Tenant

| Campo | Modelo objetivo | Estado actual | Gap |
|---|---|---|---|
| `tenant_id` | UUID fuerte que identifica `Organization x Product`. | BFF/Control Plane ya manejan tenant UUID. | Companion todavia recibe/usa `org_id` y `product_surface` en varios modulos. |
| `org_id` | Vive dentro de `Tenant`, no en cada entidad core. | Sigue presente en APIs/storage anterior. | Migracion gradual por modulo. |
| `product_surface` | Vive dentro de `Tenant`. | Sigue presente en APIs/storage anterior. | Migracion gradual por modulo. |

Decision: no reemplazar `tenant_id` por `org_id`. Son conceptos distintos.

### VirtualEmployee

| Modelo objetivo | Estado actual | Gap |
|---|---|---|
| Tabla propia `companion_virtual_employees`. | Existe. | Sin gap estructural mayor. |
| `employee_id UUID`. | Existe. | OK. |
| `tenant_id UUID`. | Existe en entidad nueva. | Falta alinear modulos relacionados. |
| Referencias por ID a `JobRole`, `EmployeeProfile`, `Capability`, `Memory`. | Existe parcialmente. | Hay transicion de perfiles/capabilities/memory anteriores. |
| Sin `agent_id`, `metadata`, `job_title`, `mission`, `responsibilities`. | Modelo nuevo evita esos campos core. | Revisar que UI/contratos no reintroduzcan duplicados. |

Siguiente accion: no tocar hasta cerrar `EmployeeProfile`, `Capability` y
`Memory` como entidades fuertes completas.

### EmployeeProfile

| Modelo objetivo | Estado actual | Gap |
|---|---|---|
| `EmployeeProfile` publico. | `/v1/employee-profiles`, `/api/employee-profiles` y Console usan nombre publico. | OK en superficie publica. |
| `profile_id UUID`. | Superficie publica usa UUID. | Revisar datos existentes/dev. |
| Storage `companion_employee_profiles`. | Storage fisico sigue siendo `agent_profiles`. | Migracion fisica pendiente. |
| Runtime puede usar profile tecnico sin exponer `AgentProfile`. | Runtime sigue teniendo nombres internos con Agent/Profile. | Aceptable tecnico, pero conviene limpiar nombres cuando no rompa runtime. |

Siguiente accion: PR de migracion fisica y rename interno controlado.

### JobRole

| Modelo objetivo | Estado actual | Gap |
|---|---|---|
| `job_role_id UUID`. | Existe alineacion parcial. | Verificar migracion y contrato final. |
| `responsibilities` estructuradas. | Existe embebido. | OK v1. |
| `recommended_capability_ids UUID[]`. | Hay transicion desde keys/capabilities recomendadas. | Normalizar contra catalogo UUID. |
| Sin permisos reales. | Se mantiene como defaults/recomendacion. | OK. |

Siguiente accion: ajustar JobRole despues de cerrar `Capability` UUID estable.

### Capability Y Tool

| Modelo objetivo | Estado actual | Gap |
|---|---|---|
| `Capability.capability_id UUID`. | Catalogo nuevo existe. | Falta eliminar uso conceptual de capability key como ID. |
| `Capability.capability_key` string. | Existe/convive. | OK como key humana. |
| Employee referencia capabilities, no tools. | En modelo nuevo si. | Revisar perfiles/runtime anteriores con tools directas. |
| Tool es tecnico/dev. | Existe como catalogo tecnico. | OK. |

Siguiente accion: normalizar referencias de Employee/Profile/JobRole a UUIDs.

### Memory

| Modelo objetivo | Estado actual | Gap |
|---|---|---|
| `Memory` como contenedor. | Existe `companion_memories`. | OK estructural. |
| `MemoryEntry` pertenece a `Memory`. | Existe. | OK v1. |
| `VirtualEmployee.memory_id`. | Existe en modelo nuevo. | Falta revisar flujos anteriores que usan scopes/conversaciones antiguas. |

Siguiente accion: conectar runtime/memory anterior al contenedor cuando el modelo
de Employee ya este estable.

### Task

| Modelo objetivo | Estado actual | Gap |
|---|---|---|
| `Task.tenant_id UUID`. | Task usa principalmente `org_id`. | Gap grande. |
| `Task.assignee_employee_id`. | Existe parcialmente en context/DTO. | Falta columna/contrato fuerte si se decide. |
| Audit comun por create/update/assign. | No completo. | Depende de tenancy limpia. |

Siguiente accion: migrar Tasks a `tenant_id` antes de audit comun completo.

### Watcher

| Modelo objetivo | Estado actual | Gap |
|---|---|---|
| `Watcher.tenant_id UUID`. | Watcher usa principalmente `org_id`. | Gap grande. |
| `Watcher.assignee_employee_id`. | Existe en DTO/config. | Falta modelo fuerte. |
| Audit comun por create/update/status. | No completo. | Depende de tenancy limpia. |

Siguiente accion: migrar Watchers a `tenant_id` y sacar assignment de config si
se vuelve relacion core.

### Handoff

| Modelo objetivo | Estado actual | Gap |
|---|---|---|
| Publico Employee -> Employee. | Existe `/v1/handoffs` y `/api/handoffs`. | OK v1. |
| Sin `from_agent_id`/`to_agent_id` publico. | Superficie publica limpia. | OK. |
| Handoffs tecnicos de agents internos. | Todavia existen. | Retirar cuando no haya consumidores. |

Siguiente accion: no prioritario salvo que aparezca consumidor nuevo.

### AuditEvent

| Modelo objetivo | Estado actual | Gap |
|---|---|---|
| `AuditEvent` generico en platform lifecycle. | Existe localmente en Platform, no consumido por Axis. | Falta version/tag. |
| Axis usa `AuditEvent`, no `ArchiveAudit`. | Axis aun usa `ArchiveAudit` por dependencia `v0.2.0`. | Bloqueado por Platform `v0.3.0`. |
| Audit comun para Workforce. | Existe adapter `companion/internal/audit`. | Falta cobertura total en Tasks/Watchers y migracion de historicos. |

Siguiente accion: versionar Platform y cambiar Axis a `AuditEvent`.

## Backlog Recomendado

### PR 1: Platform lifecycle v0.3.0

Objetivo:

```text
Publicar/taggear platform/lifecycle/go v0.3.0.
```

Criterio:

- `AuditEvent` y `AuditPort` disponibles desde version real.
- `ArchiveAudit` queda como alias temporal hasta que Axis use `AuditEvent`.
- No se agrega dominio Axis a Platform.

### PR 2: Axis usa `AuditEvent`

Objetivo:

```text
Actualizar companion/go.mod y nexus/go.mod.
Reemplazar lifecycle.ArchiveAudit por lifecycle.AuditEvent.
```

Criterio:

- No quedan referencias productivas a `ArchiveAudit`.
- Tests de Companion, Nexus y BFF pasan.

### PR 3: EmployeeProfile fisico

Objetivo:

```text
Migrar agent_profiles -> companion_employee_profiles.
Eliminar nombres publicos AgentProfile restantes.
```

Criterio:

- La API publica sigue siendo `/employee-profiles`.
- Runtime sigue resolviendo `profile_id`.
- Migraciones up/down pasan.

### PR 4: Tenant limpio en Tasks/Watchers

Objetivo:

```text
Agregar tenant_id UUID real a Tasks y Watchers.
Mantener org_id/product_surface solo como campos transitorios durante migracion.
```

Criterio:

- No se usa `tenant_id = org_id`.
- BFF resuelve tenant y Companion recibe contexto consistente.

### PR 5: Audit comun completo

Objetivo:

```text
Emitir AuditEvent para Task y Watcher create/update/status/assign.
```

Criterio:

- `GET /v1/audit-events` puede mostrar historial comun por recurso.
- No reemplaza Nexus evidence ni approvals.

### PR 6: Capability UUID end-to-end

Objetivo:

```text
Employee/Profile/JobRole referencian capabilities por UUID.
capability_key queda solo como key humana.
```

Criterio:

- UI muestra key/name.
- Payloads guardan UUID.

## No Hacer Todavia

- No mover dominio Workforce a Platform.
- No forzar `tenant_id = org_id`.
- No eliminar Agents tecnicos hasta que Runtime no los necesite.
- No eliminar endpoints tecnicos de Agents sin revisar consumidores.
- No convertir `Responsibility` en entidad propia.
- No meter PermissionBundle/IAM Role dentro de JobRole.
- No hacer backfill historico complejo de audit mientras los datos sean dev.

## Como Usar Este Reporte

Antes de implementar un PR:

1. Elegir un gap de la tabla priorizada.
2. Confirmar que no esta bloqueado.
3. Implementar solo ese gap.
4. Actualizar este documento.
5. Correr checks en Docker.

Si el codigo cambia pero este reporte no cambia, probablemente queda deuda
invisible.
