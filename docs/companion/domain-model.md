# Companion Domain Model

## Proposito Del Documento

Este documento fija el modelo conceptual de Companion dentro de Axis. Su
objetivo es separar con claridad las entidades de dominio, las entidades
operativas y las piezas tecnicas internas que sostienen una fuerza laboral
digital.

Ver tambien los specs objetivo en `../specs/companion/domain/`:

- `workforce-domain-spec.md`: mapa rector de Workforce.
- `virployees-domain-spec.md`: modelo objetivo de `Virployee`.
- `identity-and-tenancy-domain-spec.md`: `Organization`, `Product`, `Tenant`
  y `User`.
- `job-roles-domain-spec.md`: `JobRole`, `Responsibility` y criterios.
- `virployee-profiles-domain-spec.md`: modelo publico objetivo de perfiles.
- `capabilities-and-tools-domain-spec.md`: separacion `Capability` vs `Tool`.
- `memory-domain-spec.md`: `Memory` vs `MemoryEntry`.
- `agents-domain-spec.md`: `Agent` como ejecutor tecnico separado.
- `work-domain-spec.md`: `Task`, `Watcher` y `Handoff`.
- `audit-domain-spec.md`: historial fuera del core.

No todo debe ser Virployee. No todo debe ser Agent. Cada entidad existe
solo si resuelve un problema distinto.

## Principio Rector Del Dominio

Regla conceptual:

```text
JobRole define que funcion existe.
Virployee ocupa esa funcion.
Agent ejecuta tecnicamente.
VirployeeProfile configura como ejecuta.
Capability define que sabe hacer.
Tool ejecuta una funcion concreta.
Task es el trabajo.
Job es infraestructura.
Watcher observa.
Memory recuerda.
Runtime orquesta.
Nexus autoriza.
```

Companion administra IA, ejecucion, memoria, tools, agentes, empleados
digitales y trabajo asistido. Nexus administra decisiones sensibles, policies,
approvals, evidence y autorizacion fuerte.

## Separacion De Niveles

### Entidades Publicas De Dominio

Son conceptos que el usuario final o un administrador operativo puede entender
sin conocer detalles del runtime.

- `Organization`
- `Tenant / Workspace`
- `Product`
- `ProductInstallation`
- `Virployee`
- `JobRole`
- `Responsibility`
- `Task`
- `Memory` relevante
- `Watcher` cuando se opera proactividad
- `Approval` cuando aplica

### Entidades Operativas

Son conceptos visibles para admins, operadores o developers avanzados porque
configuran, gobiernan o explican la operacion.

- `VirployeeProfile`
- `Capability`
- `Adapter`
- `AssistPack`
- `RuntimePolicy`
- `Policy`
- `Evidence`
- `CostBudget`
- `SLA`
- `KPI`
- `Observability / Evals`

### Entidades Tecnicas Internas

Son piezas de implementacion. Pueden aparecer en logs, APIs compatibilidad tecnica o tooling,
pero no deberian ser el lenguaje principal del usuario final.

- `Agent`
- `Tool`
- `Runtime`
- `Planner`
- `Job`
- `AssistRun`
- `Session / Conversation` internals
- `Handoff` tecnico
- `MCP` internals
- `Secret`

## Tabla De Entidades Principales

| Entidad | Definicion | Utilidad | Ambito | CRUD | Publica/Interna | Estado actual |
|---|---|---|---|---|---|---|
| Organization | Empresa cliente o cuenta organizacional | Agrupa usuarios, workspaces y operacion | Axis/BFF | Si | Publica admin | Existe como `axis_orgs` |
| Tenant / Workspace | Contexto `org_id + product_surface` | Define donde trabaja la IA | Axis/BFF/Companion | Si | Publica admin | Existe como `axis_tenants`; nombre tenant es historico |
| Product | Superficie o producto conectado | Declara una integracion de dominio | Companion | Si | Admin/dev | Existe |
| ProductInstallation | Instalacion de Product en Organization | Configura conexion org-producto | Companion | Si | Admin/dev | Existe |
| Virployee | Trabajador digital persistente dentro de un tenant | Recurso principal que recibe trabajo | Companion | Si | Publica | Existe como wrapper v1 sobre Agent |
| JobRole | Puesto de trabajo dentro de un tenant | Define mision, responsabilidades y defaults | Companion | Si | Publica admin | Existe |
| Responsibility | Obligacion estable de un JobRole | Explica deberes esperados | Embebida en JobRole | No v1 | Publica dentro de JobRole | Existe embebida |
| Agent | Unidad tecnica de ejecucion IA | Identidad runtime, autonomia, estado y compatibilidad | Companion | Si tecnico | Interna/compatibilidad tecnica | Existe en modulo tecnico de agents |
| VirployeeProfile | Plantilla tecnica de comportamiento | Prompt, modelo, limites y allowlists | Companion | Si | Admin/dev avanzado | Existe |
| Capability | Habilidad reusable declarada por contrato | Describe que puede hacerse | Companion | Si | Admin/dev avanzado | Existe |
| Tool | Funcion tecnica invocable | Ejecuta una operacion concreta | Runtime/MCP | No como negocio | Interna/dev | Existe |
| Task | Trabajo concreto asignable y auditable | Unidad operativa de trabajo | Companion | Si | Publica/operativa | Existe |
| Job | Ejecucion background o programada | Retry, async y scheduling tecnico | Companion | No usuario | Interna | Existe |
| Watcher | Observador proactivo | Detecta eventos/condiciones y crea trabajo | Companion | Si tecnico | Admin avanzado | Existe |
| Memory | Contexto persistente | Recuerda datos utiles por scope | Companion | Si limitado | Operativa/admin | Existe |
| AssistPack | Paquete de asistencia reusable | Configura prompts/asistencia por caso | Companion | Si | Admin/dev | Existe |
| AssistRun | Ejecucion de un AssistPack | Trazar resultado de asistencia | Companion | No CRUD normal | Interna/ops | Existe |
| Runtime | Motor de orquestacion IA | Ejecuta LLM, tools, memoria y traces | Companion | No CRUD | Interna | Existe |
| RuntimePolicy | Politica tecnica de runtime | Limites, kill switches y governance | Companion | Si | Admin avanzado | Existe |
| Planner | Componente de planificacion | Descompone y coordina trabajo | Runtime | No CRUD | Interna | Existe tecnico |
| Session / Conversation | Interaccion humano/IA | Continuidad conversacional | Companion/BFF | Parcial | Operativa | Existe como chat/conversations |
| Handoff | Transferencia de ownership/contexto | Pasar trabajo entre agents/employees | Companion | Si tecnico | Operativa avanzada | Existe |
| Department / Area | Agrupacion funcional | Organizar roles y empleados | Business domain | Futuro | Publica admin | Parcial en BusinessModel |
| PermissionBundle | Paquete reutilizable de permisos | Agrupar autorizaciones | Nexus/IAM futuro | Si futuro | Admin avanzado | Falta; no en Companion |
| IAM Role | Rol humano o cuenta | Autoriza acceso humano/admin | BFF/Nexus | Si | Admin | Existe parcialmente |
| Policy | Regla de decision sensible | Autoriza, deniega o pide approval | Nexus | Si | Admin avanzado | Existe |
| Approval | Aprobacion humana/sensible | Controlar acciones riesgosas | Nexus | Si | Publica admin | Existe |
| Evidence | Evidencia para decision | Justificar approvals/actions | Nexus/Companion | No CRUD normal | Admin/interna | Existe |
| KPI | Metrica de resultado | Medir performance de empleados/roles | Futuro | No todavia | Publica futura | Falta |
| SLA | Expectativa de servicio | Definir tiempos/calidad esperada | JobRole/Policy futuro | No v1 | Publica/admin | Parcial como policy/metadata |
| CostBudget | Limite de gasto/uso | Gobernar consumo | RuntimePolicy/Ops | Si como policy | Admin | Existe parcial |
| ContactChannel | Canal de contacto/escalamiento | Contactar o escalar trabajo | Metadata/value object | No v1 | Publica | Existe como metadata |
| MCP | Superficie de tools gobernadas | Exponer tools a agentes/clientes autorizados | Companion | Si config parcial | Dev avanzado | Existe |
| Secret | Credencial sensible | Conectar productos/tools | Infra/Companion | No publico amplio | Interna/admin | Existe tecnico |

## Definicion De Cada Entidad

### Organization

Empresa cliente o cuenta organizacional. Agrupa usuarios, tenants/workspaces,
productos instalados y datos operativos. No debe representar un producto ni un
departamento.

Ejemplo: `cristo.tech`.

### Tenant / Workspace

Contexto efectivo de trabajo. En Axis, el tenant operativo de Companion es:

```text
tenant = org_id + product_surface
```

El nombre `tenant` existe por compatibilidad historica. Conceptualmente,
`Workspace` o `Work Context` describe mejor el ambito.

Ejemplo: `org_id=acme`, `product_surface=pymes`.

### Product

Superficie o producto conectado a Axis. Define una fuente de capabilities,
manifests y contratos de integracion. No debe contener logica vertical
hardcodeada en Companion.

Ejemplo: `pymes`, `medmory`.

### ProductInstallation

Instalacion concreta de un Product dentro de una Organization. Guarda datos de
conexion, estado y configuracion por org. No es el producto global.

Ejemplo: Pymes instalado para `acme`.

### Virployee

Trabajador digital persistente dentro de un tenant. Tiene identidad, ocupa un
JobRole, recibe trabajo y usa recursos disponibles para cumplir una funcion.

En v1, es el concepto publico sobre modulo tecnico de agents. No debe ser un alias vacio si
en el futuro necesita lifecycle, owner, department, reporting o contrato propio.

Ejemplo: `Billing Employee` que recibe tareas de facturacion.

### JobRole

Puesto de trabajo dentro de un tenant. Define mision, responsabilidades y
defaults del puesto. Puede sugerir capabilities, autonomia o policies
default, pero no autoriza acciones.

Ejemplo: `Billing Specialist`.

### Responsibility

Obligacion estable de un JobRole. En v1 no tiene CRUD propio; vive embebida
dentro de JobRole.

Ejemplo: `Review overdue invoices`.

### Agent

Unidad tecnica de ejecucion IA. Sigue existiendo para runtime, tooling tecnico
y compatibilidad de agentes. Resuelve identidad runtime,
estado, autonomia, audit y handoffs.

No debe ser el lenguaje principal de producto para usuarios finales.

Ejemplo: row interno en `companion_agents`.

### VirployeeProfile

Plantilla tecnica de comportamiento: prompt, modelo, limites, tools y
capabilities permitidas. No es JobRole, no es puesto de trabajo, no es perfil
humano.

Ejemplo: `axis.ops.billing.v1`.

### Capability

Habilidad reusable declarada por contrato. Explica que se puede hacer de forma
estable, auditable y reusable. No ejecuta por si misma; necesita tools o un
adapter tecnico especifico si Axis consume otro servicio.

Ejemplo: `billing.invoice.read`.

### Tool

Funcion tecnica invocable por Runtime o MCP. Es granular y
ejecutable. No debe reemplazar Capability: Tool es como se ejecuta, Capability
es que habilidad representa.

Ejemplo: `billing_read_invoice`.

### Task

Trabajo concreto asignable y auditable. Una task tiene estado, mensajes,
acciones y evidencia. No es un JobRole ni un Job tecnico.

Ejemplo: `Review invoice INV-1001`.

### Job

Ejecucion background/programada. Sirve para async, retries, workers y tareas
tecnicas. No confundir con JobRole.

Ejemplo: job de embedding de memoria.

### Watcher

Observador proactivo que detecta eventos o condiciones y crea trabajo,
propuestas o alertas. No es empleado; es automatizacion.

Ejemplo: watcher de facturas vencidas.

### Memory

Contexto persistente. Recuerda datos utiles para conversaciones, tareas o
empleados. No reemplaza la verdad transaccional del producto.

Ejemplo: preferencia de un usuario o resumen durable de conversacion.

### AssistPack

Paquete reusable de asistencia. Define prompts, instrucciones o formato para
una asistencia especifica.

Ejemplo: pack para resumir evidencia de una request.

### AssistRun

Ejecucion concreta de un AssistPack. Sirve para trazabilidad, output y
diagnostico. No necesita CRUD de usuario normal.

Ejemplo: run que genero un resumen de aprobacion.

### Runtime

Motor que orquesta IA, tools, memoria, traces y policies tecnicas. No es una
entidad de dominio visible; es infraestructura de ejecucion.

Ejemplo: orchestrator que resuelve una task con LLM y tools.

### RuntimePolicy

Politica tecnica de runtime: limites, kill switches, modelos permitidos,
budgets y restricciones. Es governance operativo, no permiso de negocio.

Ejemplo: max autonomy `A2` para una org.

### Planner

Componente interno que arma planes o descompone trabajo. No debe tener CRUD
propio hasta que existan workflows administrables.

Ejemplo: plan de pasos para resolver una task.

### Session / Conversation

Interaccion conversacional entre humano, empleado digital y sistema. Mantiene
continuidad, mensajes y contexto. No debe reemplazar Task cuando hay trabajo
auditable.

Ejemplo: conversacion con un Virployee.

### Handoff

Transferencia de ownership o contexto entre agentes/empleados. Es operativo y
tecnico. No debe ser una aprobacion ni una policy.

Ejemplo: pasar una task de billing a support.

### Department / Area

Agrupacion funcional dentro de una Organization o Workspace. Sirve para
organizar JobRoles, ownership y reporting. Existe parcialmente como contexto en
BusinessModel; no necesita CRUD fuerte todavia si no hay reporting real.

Ejemplo: `Finance`.

### PermissionBundle

Paquete reutilizable de permisos. Debe vivir cerca de IAM/Nexus, no como
autorizacion real dentro de Companion. JobRole puede referenciar un default,
pero no otorgarlo.

Ejemplo: bundle `billing-readonly`.

### IAM Role

Rol de acceso humano o de cuenta. Define que puede operar un usuario/admin en
Axis. No es JobRole.

Ejemplo: `platform_admin`, `member`.

### Policy

Regla de decision sensible. Vive en Nexus y puede permitir, denegar o pedir
aprobacion.

Ejemplo: compras mayores a cierto monto requieren approval.

### Approval

Aprobacion humana o sensible requerida por una Policy. No es una task normal,
aunque pueda aparecer asociada a trabajo.

Ejemplo: aprobar una accion de escritura en un producto.

### Evidence

Evidencia usada para justificar decisiones, approvals o acciones. Debe ser
auditable y sanitizada.

Ejemplo: datos resumidos que explican por que se pide una aprobacion.

### KPI

Metrica de resultado o performance. Es util para fuerza laboral digital, pero
no conviene crearla como entidad fuerte hasta medir tareas/resultados reales.

Ejemplo futuro: tiempo promedio de resolucion de billing tasks.

### SLA

Expectativa de tiempo, prioridad o calidad. En v1 puede vivir como policy o
metadata. Entidad propia solo cuando se mida y gobierne.

Ejemplo: resolver tareas P1 en 4 horas.

### CostBudget

Limite de gasto o uso. Puede estar dentro de RuntimePolicy u ops. Sirve para
gobernar tokens, tools y costos.

Ejemplo: presupuesto mensual de tokens por org.

### ContactChannel

Canal descriptivo para contactar o escalar. En v1 debe ser metadata/value
object, no entidad con CRUD.

Ejemplo: `slack:#billing-ops`.

### MCP

Superficie para exponer tools gobernadas a agentes/clientes autorizados. Es
tecnica y avanzada; no es el dominio principal del usuario final.

Ejemplo: MCP tool `axis.products.list`.

### Secret

Credencial o configuracion sensible. Necesaria para conectores y productos. No
debe filtrarse a logs, evidence o UI general.

Ejemplo: token API de un producto.

## Que Debe Ver El Usuario Final

El usuario final deberia ver conceptos de trabajo, no maquinaria interna:

- `Virployee`
- `JobRole`
- `Task`
- `Memory` relevante
- `Watcher` si administra proactividad
- `Approval` si debe decidir acciones sensibles

## Que Debe Ver Un Admin/Dev Avanzado

Un admin o developer avanzado puede ver configuracion y governance:

- `VirployeeProfile`
- `Capability`
- `RuntimePolicy`
- `AssistPack`
- `Evals`
- `Observability`
- `Costs`
- `Product`
- `ProductInstallation`

## Que Es Interno

Estos conceptos no deberian ser lenguaje principal de producto:

- `Agent`
- `Runtime`
- `Planner`
- `Tool`
- `Job`
- `Trace`
- `AssistRun`
- `MCP` internals
- `Secret`

## Relaciones Principales

```text
Organization
└── Tenant / Workspace (org_id + product_surface)
    ├── ProductInstallation
    ├── Department / Area
    │   └── JobRole
    │       └── Responsibility[]
    ├── Virployee
    │   ├── occupies JobRole
    │   ├── uses Agent / VirployeeProfile internally
    │   ├── has Memory
    │   ├── receives Tasks
    │   └── may use Capabilities
    ├── Task
    ├── Watcher
    ├── Job
    ├── RuntimePolicy
    ├── CostBudget
    └── Observability / Evals

Product
└── Capabilities
    └── Tools

Nexus
├── Policies
├── Approvals
└── Evidence
```

## Entidades Que Ya Existen Bien

- `Task`: unidad de trabajo concreta.
- `Capability`: habilidad reusable por contrato.
- `Memory`: contexto persistente con scope.
- `RuntimePolicy`: governance tecnico.
- `Watcher`: proactividad.
- `AssistPack / AssistRun`: asistencia reusable y trazabilidad.
- `Product / ProductInstallation`: base multi-producto.
- `Organization / Tenant`: aislamiento y contexto de trabajo.
- `JobRole`: puesto de trabajo separado de permisos.
- `Virployee`: concepto publico, aunque v1 use modulo tecnico de agents.

## Entidades Con Nombres Confusos

- `Agent`: correcto tecnicamente, confuso como concepto principal de producto.
- `Tenant`: historico; conceptualmente es `Workspace` o `Work Context`.
- `Role`: palabra sobrecargada entre IAM Role, business role y JobRole.
- `Job`: infraestructura tecnica, no puesto de trabajo.
- `VirployeeProfile`: plantilla tecnica, no perfil laboral.
- `BusinessModel.roles`: contexto descriptivo, no JobRole operativo.

## Entidades Que Faltan

- `PermissionBundle`: util, pero deberia vivir en IAM/Nexus o contratos de
  autorizacion, no como enforcement en Companion.
- `KPI / Scorecard`: util cuando existan metricas reales de resultado.
- `SLA Policy` fuerte: util cuando se mida y aplique operacionalmente.
- `Workflow` operativo: util cuando haya procesos administrables, no solo
  planner interno.
- `Department / Area` operativo: util cuando haya reporting/ownership real.

## Entidades Que NO Conviene Crear Todavia

Por ahora no se recomienda crear:

- `Responsibility` con CRUD propio.
- tabla `virployees`.
- `Multi-agent Employee`.
- `KPI` como entidad fuerte.
- `Department` CRUD.
- `ContactChannel` CRUD.
- `PermissionBundle` dentro de Companion.

Algunas pueden aparecer mas adelante si hay necesidad real: reporting,
ownership, canales reales, permisos reutilizables, SLAs medidos o empleados
compuestos.

## Reglas De Diseno

- `JobRole` NO es `IAM Role`.
- `Job` NO es `JobRole`.
- `VirployeeProfile` NO es `JobRole`.
- `Agent` NO es `Virployee`.
- `Tool` NO es `Capability`.
- `Memory` NO es truth transaccional.
- `JobRole` puede sugerir defaults, pero NO autoriza acciones.
- `PermissionBundle`, IAM y Nexus autorizan.
- Companion no debe decidir risk/policy fuerte.
- Nexus no debe importar runtime LLM ni memoria IA.
- Productos externos son fuente de verdad de su dominio.
- Metadata sirve para v1, pero no debe ocultar entidades que necesiten
  lifecycle propio.
- Crear una entidad solo si aporta lifecycle, ownership, relaciones,
  validacion o reporting propios.

## Glosario Corto

- `org_id`: customer org donde ocurre el trabajo.
- `product_surface`: producto o superficie conectada.
- `tenant`: termino historico; en Companion significa `org_id +
  product_surface`.
- `Virployee`: trabajador digital persistente.
- `JobRole`: puesto que ocupa un Virployee.
- `Agent`: implementacion tecnica de ejecucion.
- `Capability`: habilidad reusable.
- `Tool`: funcion invocable.
- `Task`: trabajo concreto.
- `Job`: ejecucion tecnica background.
- `Watcher`: observador proactivo.
- `Memory`: contexto persistente.
- `Runtime`: motor tecnico de ejecucion IA.
- `Nexus`: servicio que autoriza decisiones sensibles.
