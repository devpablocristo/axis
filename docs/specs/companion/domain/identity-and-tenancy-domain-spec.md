# Identity And Tenancy Domain Spec

## Proposito

Este spec define las entidades de identidad y tenancy que dan ambito a
Workforce. No describe el almacenamiento actual completo del BFF ni de
Companion.

## Organization

Definicion: cuenta organizacional cliente.

Utilidad: agrupa tenants, usuarios y operacion administrativa.

No representa: producto, tenant, departamento ni empleado.

Tipo: entidad fuerte.

CRUD objetivo: si.

Audiencia: publica admin.

Modelo objetivo:

```text
Organization
- organization_id: UUID
- name: string
- slug: string
- status: OrganizationStatus
```

Enums:

```text
OrganizationStatus: active, suspended, archived
```

Relaciones:

```text
Tenant.organization_id -> Organization.organization_id
```

Estado actual: existe como `axis_orgs` en BFF/IAM. Algunos contratos actuales
usan `org_id` como string.

Brecha: `org_id` actual debe mapear al `organization_id` objetivo o quedar como
key externa/historica.

## Product

Definicion: producto o superficie conectable a Axis.

Utilidad: identifica el dominio externo donde trabaja un tenant y de donde
pueden venir capabilities.

No representa: tenant, instalacion ni connector concreto.

Tipo: entidad fuerte.

CRUD objetivo: si.

Audiencia: publica admin/dev.

Modelo objetivo:

```text
Product
- product_id: UUID
- product_key: string
- name: string
- status: ProductStatus
```

Enums:

```text
ProductStatus: active, disabled, archived
```

Relaciones:

```text
Tenant.product_id -> Product.product_id
Capability.product_id -> Product.product_id
```

Estado actual: existe product catalog con `product_surface` string.

Brecha: `product_surface` debe ser `product_key`; `product_id` debe ser UUID.

## Tenant

Definicion: instancia de trabajo de una organizacion en un producto.

Utilidad: define donde vive un `VirtualEmployee` y donde se scopen datos
operativos.

No representa: organization sola, product solo ni permisos.

Tipo: entidad fuerte.

CRUD objetivo: si.

Audiencia: publica admin.

Modelo objetivo:

```text
Tenant
- tenant_id: UUID
- organization_id: UUID
- product_id: UUID
- name: string
- status: TenantStatus
```

Enums:

```text
TenantStatus: active, suspended, archived
```

Relaciones:

```text
VirtualEmployee.tenant_id -> Tenant.tenant_id
JobRole.tenant_id -> Tenant.tenant_id
Memory.tenant_id -> Tenant.tenant_id
Task.tenant_id -> Tenant.tenant_id
```

Estado actual: existe `axis_tenants.id` en BFF. Hoy resuelve
`org_id + product_surface`.

Brecha: el modelo objetivo usa `tenant_id` en Employees. `org_id` y
`product_surface` quedan dentro de Tenant como implementacion actual, no como
campos de `VirtualEmployee`.

## User

Definicion: humano que opera, supervisa o administra dentro de un tenant.

Utilidad: permite asignar responsabilidad humana, ownership operativo y acceso.

No representa: empleado virtual, agente ni service principal.

Tipo: entidad fuerte.

CRUD objetivo: si.

Audiencia: publica admin.

Modelo objetivo:

```text
User
- user_id: UUID
- tenant_id: UUID
- name: string
- email: string
- status: UserStatus
```

Enums:

```text
UserStatus: invited, active, disabled, archived
```

Relaciones:

```text
VirtualEmployee.supervisor_user_id -> User.user_id
AuditEvent.actor_user_id -> User.user_id | null
```

Estado actual: existen usuarios y memberships en BFF/IAM, con IDs string.

Brecha: `supervisor_user_id` objetivo debe apuntar a `User.user_id`; no es
`owner_user_id`, creador ni admin necesariamente.

## Auditoria Actual Vs Objetivo

| Concepto actual | Problema | Modelo objetivo |
|---|---|---|
| `org_id + product_surface` | Se usa como tenant operativo en muchos contratos. | `tenant_id` UUID. |
| `product_surface` | Es key textual de producto. | `product_key`; `product_id` UUID. |
| `axis_tenants.id` string | Generado como `tenant_xxx` en memoria/dev. | UUID canonico. |
| `owner_user_id` | Nombre ambiguo: owner, creador o admin. | `supervisor_user_id` para el responsable operativo. |

