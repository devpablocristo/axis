# Tools

Las tools son capacidades internas que el LLM puede solicitar durante un run.
El runtime filtra los schemas antes de enviarlos al modelo.

## Tools actuales

| Tool | Uso | Requisitos |
|---|---|---|
| `get_overview` | Resumen operativo por customer org | customer org requerida |
| `check_approvals` | Aprobaciones pendientes en Nexus | customer org + `companion:nexus:admin` |
| `list_policies` | Policies de Nexus | customer org + `companion:nexus:admin` |
| `list_watchers` | Watchers de la customer org | customer org + `companion:watchers:read` |
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

Los connectors pertenecen a una customer org: una fila `org_id=''` no autoriza
ejecución de trabajo.
Los templates estáticos del registry solo publican schemas.

Las capabilities publicadas por connectors son la fuente de tools específicas
por producto. Pymes y Ponti son adapters/configuración; el runtime común no debe
asumir un negocio concreto como default conceptual.
