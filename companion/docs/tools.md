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
- La configuración runtime de la customer org puede filtrar tools, connectors
  y capabilities antes de que el LLM las vea o las ejecute.

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

## Capability manifests

Cada capability se normaliza a `capability_manifest.v1` antes de exponerse al
LLM, enviarse a Nexus o ejecutarse por connectors. El manifest canónico incluye:

- identidad versionada: `capability_id`, `version`, `owner`, `product_surface`,
  `connector`;
- contrato: `input_schema`, `output_schema`, `evidence_schema`,
  `required_evidence`, preconditions y postconditions;
- control operativo: `action_type`, `risk_level`, `side_effect_type`,
  `auth_mode`, scopes, timeout, retries, cost/rate class;
- control de acciones sensibles: `nexus_action_type`, `approval_required`,
  idempotency mode y rollback/compensation strategy.

El loader de manifests nuevos es estricto: schemas con `required` apuntando a
propiedades inexistentes se rechazan. Para compatibilidad, los connectors historical
que solo declaran `required` se adaptan a schemas completos antes de registrarse.

Endpoint operativo:

- `GET /v1/connectors/capability-manifests`: devuelve los manifests efectivos
  filtrados por identidad, scopes, riesgo e `include_writes`.

Invariantes:

- Companion decide qué capability quiere usar, no decide el resultado final de
  acciones críticas.
- Los writes deben declarar `approval_required=true` y `nexus_action_type`.
- Una capability con rollback automático debe declarar `rollback_capability_id`.
- La metadata que ve el planner y la metadata enviada a Nexus derivan del mismo
  manifest versionado.
- Si existe runtime control plane y falta una política de customer org, la
  ejecución de capabilities falla cerrado.
