# Agents

Companion usa perfiles globales persistidos en `agent_profiles`, Agent Fleet en
`companion_agents` y enforcement en `internal/runtime`.

## Modelo actual

Cada run produce:

- `IdentityChain`: customer org, usuario humano/delegado, product surface,
  scopes y principal tecnico `companion.employee_ai`.
- `AgentRoute`: intención clasificada, producto, autonomía efectiva y allowed
  tools.
- `AgentProfile`: perfil efectivo versionado, prompt de agente, autonomía
  máxima, allowlist de tools/capabilities, memory policy y scopes requeridos.
- `agent_id`: empleado IA persistente opcional, resuelto desde `internal/agentfleet`.

El routing sigue siendo determinístico y simple. Si un agente persistido trae
`profile_id`, el runtime carga ese perfil desde Postgres; si no existe, está
disabled o archivado, falla cerrado antes de llamar al LLM.

## Agent Fleet

`internal/agentfleet` agrega empleados IA persistentes por customer org y
product surface. Cuando `/v1/chat` recibe `agent_id`, el runtime carga el agente,
aplica sus límites de autonomía/tools/capabilities y escribe `agent_id` en
traces, observability y task context.

El prompt reusable del agente no vive en Medmory ni en Agent Fleet: vive en
`agent_profiles`. Ejemplo: `billing_agent` referencia `axis.ops.billing.v1`.

Los handoffs entre agentes quedan persistidos y auditados, pero no sustituyen a
Nexus ni ejecutan side effects por sí mismos.

## Autonomía

Niveles soportados: `A0` a `A5`. Default: `A2`. La autonomía no reemplaza a
Nexus: una acción sensible sigue requiriendo nexus aunque el agent tenga
mayor autonomía.

## Próxima evolución

- Overrides por producto/customer org cuando sea necesario, sin entregar
  administracion del runtime global a clientes.
- Rollout por version de perfil y panel operativo en Console/BFF.
