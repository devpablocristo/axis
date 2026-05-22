# Tools

Las tools son capacidades internas que el LLM puede solicitar durante un run.
El runtime filtra los schemas antes de enviarlos al modelo.

## Tools actuales

| Tool | Uso | Requisitos |
|---|---|---|
| `get_overview` | Resumen operativo por tenant | tenant requerido |
| `check_approvals` | Aprobaciones pendientes en Nexus | tenant + `companion:governance:admin` |
| `list_policies` | Policies de Nexus | tenant + `companion:governance:admin` |
| `list_watchers` | Watchers del tenant | tenant + `companion:watchers:read` |
| `remember` | Guarda preferencia/hecho | user u org válido |
| `recall` | Recupera memoria | user u org válido |

## Reglas

- El LLM solo recibe schemas permitidos por `AgentProfile`, `AgentRoute` e
  `IdentityChain`.
- Una tool fuera de allowlist se rechaza con guardrail `tool_policy`.
- Prompt injection en args se rechaza antes de ejecutar la tool.
- Tools de memoria no caen en scopes globales.
- Tools que consultan Nexus no sustituyen decisions de Nexus.

## Acciones sensibles

Las writes/side effects deben ejecutarse por connectors/capabilities y pasar por
Nexus antes de ejecutar. El runtime no debe tener tools directas para approve,
reject o writes sensibles sin gate.

Los connectors son tenant-owned: una fila `org_id=''` no autoriza ejecución.
Los templates estáticos del registry solo publican schemas.
