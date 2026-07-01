# Agent Fleet

Agent Fleet es el modulo tecnico que administra agentes persistentes por
`org_id` y `product_surface` sin hardcodear verticales ni mover decisiones de
acciones sensibles fuera de Nexus.

No es el dominio publico de Virployees. Los endpoints `/v1/agents`
quedan para agentes tecnicos, compatibilidad y runtime. Ver
`virployees.md` para la entidad publica `Virployee`.

El modelo objetivo ya no trata Agent Fleet como dominio publico. Ver
`../specs/companion/domain/agents-domain-spec.md` para `Agent` como ejecutor
tecnico y `../specs/companion/domain/virployees-domain-spec.md` para
`Virployee`.

## Modelo

Cada agente tiene:

- `agent_id`, nombre visible, rol y `profile_id`;
- estado `active` o `disabled`;
- autonomía máxima `A0`..`A5`;
- allowlists de tools, capabilities y connectors;
- `memory_scope_id` y política de memoria compartida;
- límites operativos, SLA y metadata;
- versión y audit trail.

La configuración vive en `companion_agents`. Cada update incrementa versión y
registra una entrada en `companion_agent_audit`.

## Runtime

`/v1/chat` acepta `agent_id`. Si se informa:

1. Companion resuelve el agente activo para la customer org y product surface.
2. El runtime agrega `agent_id` a `IdentityChain`.
3. La autonomía efectiva se reduce al máximo permitido por el agente.
4. Las tools/capabilities visibles al LLM se intersectan con la allowlist del
   agente.
5. El control plane de la org puede volver a bloquear el agente, perfil, tool o
   capability.

Si el agente no existe, está deshabilitado o el resolver no está configurado,
el runtime falla cerrado antes de llamar al modelo.

## APIs

Superficie tecnica:

- `GET /v1/agents`: lista agentes de la customer org.
- `GET /v1/agents/{agent_id}`: obtiene un agente.
- `PUT /v1/agents/{agent_id}`: crea o actualiza un agente.
- `POST /v1/agents/{agent_id}/disable`: apaga un agente.
- `GET /v1/agents/handoffs`: lista handoffs recientes.
- `POST /v1/agents/handoffs`: crea un handoff.
- `PATCH /v1/agents/handoffs/{id}`: acepta, rechaza, completa o cancela un
  handoff.

Requiere `companion:runtime:admin` o `companion:cross_org`.

## Handoffs

Los handoffs viven en `companion_agent_handoffs`. Validan que el agente destino
exista y esté activo en la misma customer org/product surface. El estado es
auditable y sirve como primitive de coordinación; no ejecuta acciones de dominio
por sí mismo.

## Frontera con Nexus

Agent Fleet solo define que agente tecnico puede operar y con que limites. Nexus
sigue decidiendo allow/deny/approval para acciones sensibles, approvals,
evidence y auditoría de side effects.
