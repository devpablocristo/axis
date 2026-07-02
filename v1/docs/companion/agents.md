# Agents

Companion usa perfiles de runtime persistidos internamente y agents tecnicos en
`companion_agents` para compatibilidad de ejecucion.

**Virployee** es el concepto publico de dominio y **Agent** es la
superficie tecnica de runtime. `virployee_id` no mapea a `agent_id`; Runtime
acepta `virployee_id` para Employees y conserva `agent_id` para compatibilidad
tecnica.

El modelo objetivo separa ambos conceptos. Ver
`../specs/companion/domain/agents-domain-spec.md` y
`../specs/companion/domain/virployees-domain-spec.md`.

## Modelo actual

Cada run produce:

- `IdentityChain`: customer org, usuario humano/delegado, product surface,
  scopes y principal tecnico `companion.employee_ai`.
- `AgentRoute`: intención clasificada, producto, autonomía efectiva y allowed
  tools.
- `VirployeeProfile`: perfil efectivo versionado, prompt, autonomía
  máxima, allowlist de tools/capabilities, memory policy y scopes requeridos.
- `agent_id`: Agent tecnico persistente opcional, resuelto desde `internal/agentfleet`.

El routing sigue siendo determinístico y simple. Si un agente persistido trae
`profile_id`, el runtime carga ese perfil desde Postgres; si no existe, está
disabled o archivado, falla cerrado antes de llamar al LLM.

## Agents Tecnicos

`internal/agentfleet` agrega Agents persistentes por customer org y product
surface. Cuando `/v1/chat` recibe `agent_id`, el runtime carga el agente,
aplica sus límites de autonomía/tools/capabilities y escribe `agent_id` en
traces, observability y task context.

El prompt reusable no vive en Medmory ni en el modulo tecnico de agents: vive
en Virployee Profiles. Ejemplo: `billing_agent` puede referenciar
`axis.ops.billing.v1` como `profile_key` tecnico.

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
