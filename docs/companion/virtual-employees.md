# Virtual Employees

## Definicion

Un Virtual Employee es un trabajador digital persistente con identidad propia,
al que se le puede asignar trabajo, contactar e interactuar, que ocupa un
puesto dentro de un tenant y es responsable de cumplir una funcion de forma
autonoma o asistida usando cualquier recurso disponible.

En Axis:

```text
tenant = org_id + product_surface
```

`org_id` representa la customer org donde trabaja Companion. `product_surface`
representa la superficie/producto conectado donde ese trabajo ocurre.

## Por Que Existe

Virtual Employee es el concepto publico de dominio que el usuario opera. El
usuario no deberia necesitar entender la maquinaria interna de runtime,
profiles, routing o Agent Fleet para asignar trabajo a un trabajador digital.

El concepto existe para separar dos preocupaciones:

- **Virtual Employee**: abstraccion de producto y dominio.
- **Agent / Agent Fleet**: implementacion tecnica actual en Companion.

Esto permite exponer una experiencia estable de trabajador digital sin romper
los contratos tecnicos existentes que todavia usan agents.

## Virtual Employee Vs Agent

| Concepto | Responsabilidad | Audiencia |
|---|---|---|
| Virtual Employee | Representar un trabajador digital persistente con puesto, mision, owner, limites y trabajo asignable dentro de `org_id + product_surface`. | Producto, Console, usuarios operativos y contratos publicos nuevos. |
| Agent | Resolver ejecucion interna: identidad runtime, perfil, autonomia, allowlists, estado, audit, handoffs y compatibilidad con `/v1/chat`. | Companion internals, runtime, integraciones legacy y tooling tecnico. |

Regla de naming:

```text
Virtual Employee = concepto publico de dominio
Agent / Agent Fleet = implementacion interna v1
```

Un Virtual Employee no reemplazo completamente a Agent. En v1, lo envuelve.

## Implementacion V1

VirtualEmployee v1 mapea 1:1 a Agent Fleet:

```text
VirtualEmployee v1 -> companion_agents row
employee_id -> agent_id
tenant -> org_id + product_surface
```

No hay tabla `virtual_employees` en v1. La persistencia sigue siendo
`companion_agents`, con audit/versioning existente de Agent Fleet.

Runtime sigue usando `agent_id` internamente. Por ejemplo, `/v1/chat` continua
aceptando `agent_id` y el resolver de flota aplica los limites, profile,
allowlists y estado del agente persistente.

## APIs Publicas Recomendadas

Companion:

- `GET /v1/virtual-employees`
- `GET /v1/virtual-employees/{employee_id}`
- `PUT /v1/virtual-employees/{employee_id}`
- `DELETE /v1/virtual-employees/{employee_id}`
- `POST /v1/virtual-employees/{employee_id}/disable`
- `POST /v1/virtual-employees/{employee_id}/archive`
- `POST /v1/virtual-employees/{employee_id}/trash`
- `POST /v1/virtual-employees/{employee_id}/restore`
- `POST /v1/virtual-employees/{employee_id}/approve`
- `POST /v1/virtual-employees/{employee_id}/ignore`
- `POST /v1/virtual-employees/assignments`
- `GET /v1/virtual-employees/handoffs`
- `POST /v1/virtual-employees/handoffs`
- `PATCH /v1/virtual-employees/handoffs/{id}`

BFF / Console:

- `/api/virtual-employees`
- `/api/virtual-employees/{employee_id}` para update via `PUT`/`PATCH`
- `/api/virtual-employees/{employee_id}/archive`
- `/api/virtual-employees/{employee_id}/trash`
- `/api/virtual-employees/{employee_id}/restore`
- `/api/virtual-employees/{employee_id}/approve`
- `/api/virtual-employees/{employee_id}/ignore`
- `/api/virtual-employees/{employee_id}/purge`

La Console debe usar Virtual Employees como recurso principal. BFF v1 sigue el
CRUD historico de `/api/agents`; no agrega un endpoint detail separado si la
superficie legacy no lo tenia.

## Endpoints De Compatibilidad Tecnica

Estos endpoints siguen existiendo y no deben eliminarse sin migracion explicita:

- `/v1/agents`
- `/v1/agents/{agent_id}`
- `/v1/agents/assignments`
- `/v1/agents/handoffs`
- `/api/agents`

Uso recomendado:

- Producto/UX nueva: usar Virtual Employees.
- Runtime e integraciones tecnicas existentes: pueden seguir usando Agents.
- Migraciones futuras: mantener compatibilidad hasta que todos los consumidores
  hayan pasado al contrato publico nuevo.

## Metadata Semantica V1

V1 usa metadata plana sobre el Agent interno para datos semanticos de Virtual
Employee:

```json
{
  "metadata": {
    "job_role_id": "billing-specialist",
    "job_title": "Billing Specialist",
    "mission": "Keep customer billing healthy",
    "responsibilities": ["review invoices", "escalate blockers"],
    "owner_user_id": "user-123",
    "contact_channels": ["slack:#billing-ops"],
    "escalation_rules": ["manager after 2 business days"]
  }
}
```

Campos:

- `metadata.job_title`: puesto visible.
- `metadata.job_role_id`: referencia v1 al JobRole que ocupa el Virtual Employee.
- `metadata.mission`: mision o funcion principal.
- `metadata.responsibilities`: lista simple de responsabilidades.
- `metadata.owner_user_id`: owner humano responsable.
- `metadata.contact_channels`: referencias descriptivas a canales de contacto.
- `metadata.escalation_rules`: reglas descriptivas de escalamiento.

Esto es un contrato v1 pragmatico, no necesariamente el modelo final. No son
columnas, no tienen validacion fuerte y no crean entidades separadas.

## Relacion Con JobRole

`JobRole` es el puesto de trabajo que puede ocupar un Virtual Employee dentro
de `org_id + product_surface`.

En v1, JobRole tiene tabla propia y CRUD propio, pero la relacion con Virtual
Employee se guarda como `metadata.job_role_id` para no migrar Agent Fleet ni
cambiar Runtime.

JobRole puede sugerir defaults como mision, responsabilidades, capabilities
recomendadas y autonomia. No es un IAM Role, no es un PermissionBundle y no
autoriza acciones directamente.

## Fuera De Alcance V1

Todavia no existe:

- tabla `virtual_employees`;
- Role CRUD;
- Responsibilities como entidad;
- Department como entidad;
- KPIs propios;
- SLA avanzado;
- canales reales de contacto;
- cola propia nueva;
- multi-agent employee;
- cambio de Runtime;
- reemplazo completo de Agents.

Tambien queda fuera que un Virtual Employee autoasigne permisos, cambie su
autonomia, modifique policies o saltee Nexus. La autorizacion sensible sigue
fuera de Companion.

## Roadmap Conceptual

Evoluciones posibles, no comprometidas en v1:

- tabla dedicada `virtual_employees` si el concepto necesita lifecycle, owner,
  department o reporting propios;
- `role`/puesto como entidad gobernada;
- responsabilidades versionadas con ownership y evidencia;
- integracion real de canales de contacto;
- reglas de escalamiento ejecutables;
- KPIs y scorecards operativos;
- relacion explicita con departments;
- empleados compuestos por multiples agents internos;
- migracion progresiva para que Runtime acepte `employee_id` como concepto
  publico y traduzca a `agent_id` internamente.

La regla para avanzar es que Employee aporte semantica y lifecycle propios. Si
solo cambia el nombre, debe seguir siendo una capa publica sobre Agent Fleet.
