# Agents

Companion usa un control plane con perfiles seedables en `internal/agents` y
enforcement en `internal/runtime`.

## Modelo actual

Cada run produce:

- `IdentityChain`: customer org, usuario humano/delegado, product surface,
  scopes y principal tecnico `companion.employee_ai`.
- `AgentRoute`: intención clasificada, producto, autonomía efectiva y allowed
  tools.
- `AgentProfile`: perfil efectivo versionado, autonomía máxima, allowlist de
  tools, memory policy y scopes requeridos.
- `agent_id`: empleado IA persistente opcional, resuelto desde `internal/agentfleet`.

El routing sigue siendo determinístico y simple. El registry actual es seedable
en código, pero la configuración runtime por customer org ya puede permitir,
denegar o apagar perfiles/agents desde `control_plane`.

## Agent Fleet

`internal/agentfleet` agrega empleados IA persistentes por customer org y
product surface. Cuando `/v1/chat` recibe `agent_id`, el runtime carga el agente,
aplica sus límites de autonomía/tools/capabilities y escribe `agent_id` en
traces, observability y task context.

Los handoffs entre agentes quedan persistidos y auditados, pero no sustituyen a
Nexus ni ejecutan side effects por sí mismos.

## Autonomía

Niveles soportados: `A0` a `A5`. Default: `A2`. La autonomía no reemplaza a
Nexus: una acción sensible sigue requiriendo nexus aunque el agent tenga
mayor autonomía.

## Próxima evolución

Evolución pendiente:

- Persistir perfiles editables por producto/customer org cuando sea necesario,
  sin entregar administracion del runtime global a clientes.
- Agregar rollout por versión de perfil y panel operativo en Console/BFF.
