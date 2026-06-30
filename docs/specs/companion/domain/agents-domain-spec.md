# Agents Domain Spec

## Proposito

Este spec define `Agent` como entidad tecnica separada de
`VirtualEmployee`.

Regla:

```text
Si es Employee, se llama Employee.
Si es Agent, se llama Agent.
```

## Agent

Definicion: ejecutor tecnico/inteligente usado por el runtime.

Utilidad: ejecutar una tarea, plan, tool call o conversacion bajo configuracion
tecnica.

No representa: trabajador digital persistente, puesto, supervisor humano ni
contrato publico principal.

Tipo: entidad fuerte tecnica.

CRUD objetivo: si tecnico, no como superficie primaria de producto.

Audiencia: interna/dev.

Modelo objetivo:

```text
Agent
- agent_id: UUID
- employee_id: UUID | null
- profile_id: UUID
- status: AgentStatus
```

Enums:

```text
AgentStatus: idle, running, disabled, error
```

Relaciones:

```text
Agent.employee_id -> VirtualEmployee.employee_id | null
Agent.profile_id -> EmployeeProfile.profile_id
```

Reglas:

- `employee_id` puede ser null para agents tecnicos no asociados a un employee.
- Un `VirtualEmployee` puede usar agents internamente, pero no es un Agent.
- `Agent` no debe aparecer como nombre publico cuando el usuario opera
  employees.

Estado actual: existe `companion_agents` y `internal/agentfleet` como storage y
modulo tecnico de Agents. VirtualEmployee tiene entidad propia.

Brecha: `companion_agents` todavia conserva campos tecnicos historicos
(`org_id + product_surface`, profile text, allowlists) que deben quedar
encapsulados como contrato tecnico de Agent.

## Handoff Tecnico Actual

Estado actual: `companion_agent_handoffs` usa `from_agent_id` y `to_agent_id`.

Modelo objetivo de dominio operativo: `Handoff` apunta a employees. Si el
runtime necesita handoff tecnico entre agents, debe ser interno.

## Auditoria Actual Vs Objetivo

| Concepto actual | Problema | Modelo objetivo |
|---|---|---|
| `agent_id text` | ID textual generado desde nombre. | `agent_id UUID`. |
| `profile_id text` | Key semantica. | `profile_id UUID`. |
| `org_id + product_surface` | Tenant implicito. | `employee_id` o `tenant_id` segun entidad. |
| `metadata_json` | Contiene datos de Employee. | Datos de Employee en entidades fuertes. |
