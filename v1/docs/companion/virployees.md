# Virployees

Ver tambien `domain-model.md` para el mapa rector del dominio de Companion y
la separacion entre Virployee, JobRole, Agent y VirployeeProfile.

Este documento describe la superficie v1 actual. El modelo objetivo de dominio
esta en `../specs/companion/domain/virployees-domain-spec.md`. El mapa
completo de entidades relacionadas esta en
`../specs/companion/domain/workforce-domain-spec.md`.

## Definicion

Un Virployee es un trabajador digital persistente con identidad propia,
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

Virployee es el concepto publico de dominio que el usuario opera. El
usuario no deberia necesitar entender la maquinaria interna de runtime,
routing o agents tecnicos para asignar trabajo a un trabajador digital.

El concepto existe para separar dos preocupaciones:

- **Virployee**: abstraccion de producto y dominio.
- **Agent**: ejecucion tecnica y compatibilidad de runtime.

Esto permite exponer una experiencia estable de trabajador digital sin romper
los contratos tecnicos existentes que todavia usan agents.

## Virployee Vs Agent

| Concepto | Responsabilidad | Audiencia |
|---|---|---|
| Virployee | Representar un trabajador digital persistente con puesto, mision, owner, limites y trabajo asignable dentro de `org_id + product_surface`. | Producto, Console, usuarios operativos y contratos publicos nuevos. |
| Agent | Resolver ejecucion interna: identidad runtime, perfil, autonomia, allowlists, estado, audit, handoffs y compatibilidad con `/v1/chat`. | Companion internals, runtime, integraciones compatibilidad tecnica y tooling tecnico. |

Regla de naming:

```text
Virployee = concepto publico de dominio
Agent = ejecutor tecnico y compatibilidad de runtime
```

Un Virployee no es un Agent renombrado. El Virployee es el trabajador
digital persistente; el Agent es un ejecutor tecnico que el runtime puede usar.

## Implementacion V1

Virployee v1 ya tiene entidad propia:

```text
Virployee -> companion_virployees row
virployee_id -> UUID publico del Virployee
tenant actual -> org_id + product_surface
```

La persistencia vive en `companion_virployees`, con capacidades
referenciadas por `companion_virployee_capabilities` y audit separado en
`companion_virployee_audit`.

Runtime acepta `virployee_id` en superficies nuevas y todavia puede usar
`agent_id` para flujos tecnicos/compatibilidad.

## APIs Publicas Recomendadas

Companion:

- `GET /v1/virployees`
- `POST /v1/virployees`
- `GET /v1/virployees/{virployee_id}`
- `PATCH /v1/virployees/{virployee_id}`
- `POST /v1/virployees/{virployee_id}/status`

BFF / Console:

- `/api/virployees`
- `/api/virployees/{virployee_id}`
- `/api/virployees/{virployee_id}/status`

La Console debe usar Virployees como recurso principal. Las rutas
historicas de `/api/agents` quedan para agentes tecnicos y compatibilidad.

## Endpoints De Compatibilidad Tecnica

Estos endpoints siguen existiendo y no deben eliminarse sin migracion explicita:

- `/v1/agents`
- `/v1/agents/{agent_id}`
- `/v1/agents/assignments`
- `/v1/agents/handoffs`
- `/api/agents`

Uso recomendado:

- Producto/UX nueva: usar Virployees.
- Runtime e integraciones tecnicas existentes: pueden seguir usando Agents.
- Migraciones futuras: mantener compatibilidad hasta que todos los consumidores
  hayan pasado al contrato publico nuevo.

## Modelo Core V1

El contrato publico de Virployee evita campos duplicados que pertenecen a otras
entidades fuertes:

```text
virployee_id
tenant_id
name
supervisor_user_id
status
job_role_id
profile_id
autonomy
capability_ids
memory_id
```

`job_title`, `mission` y `responsibilities` pertenecen a `JobRole`.
`memory_enabled` y `memory_scope_id` se reemplazan por `memory_id`.
`allowed_tools` no forma parte del Virployee: el Virployee referencia
capabilities.

## Relacion Con JobRole

`JobRole` es el puesto de trabajo que ocupa un Virployee dentro del
tenant.

En v1 la relacion vive en `Virployee.job_role_id`. El JobRole tiene tabla
propia y CRUD propio.

JobRole puede sugerir defaults como mision, responsabilidades, capabilities
recomendadas y autonomia. No es un IAM Role, no es un PermissionBundle y no
autoriza acciones directamente.

## Fuera De Alcance V1

Todavia no existe:

- Role CRUD;
- Responsibilities como entidad;
- Department como entidad;
- KPIs propios;
- SLA avanzado;
- canales reales de contacto;
- cola propia nueva;
- multi-agent virployee;
- cambio de Runtime;
- reemplazo completo de Agents.

Tambien queda fuera que un Virployee autoasigne permisos, cambie su
autonomia, modifique policies o saltee Nexus. La autorizacion sensible sigue
fuera de Companion.

## Roadmap Conceptual

Evoluciones posibles, no comprometidas en v1:

- `role`/puesto como entidad gobernada;
- responsabilidades versionadas con ownership y evidencia;
- integracion real de canales de contacto;
- reglas de escalamiento ejecutables;
- KPIs y scorecards operativos;
- relacion explicita con departments;
- empleados compuestos por multiples agents internos;
- migracion progresiva para que Runtime acepte `virployee_id` como concepto
  publico y traduzca a `agent_id` internamente.

La regla para avanzar es que Virployee aporte semantica y lifecycle propios. Si
un concepto es solo ejecucion tecnica, debe quedarse en Agent.
