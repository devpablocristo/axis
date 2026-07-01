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

- El LLM solo recibe schemas permitidos por `VirployeeProfile`, `AgentRoute` e
  `IdentityChain`.
- Una tool fuera de allowlist se rechaza con guardrail `tool_policy`.
- Prompt injection en args se rechaza antes de ejecutar la tool.
- Tools de memoria no caen en scopes globales.
- Tools que consultan Nexus no sustituyen decisions de Nexus.
- La configuración runtime de la customer org puede filtrar tools y capabilities
  antes de que el LLM las vea o las ejecute.

## Acciones sensibles

Las writes/side effects deben pasar por Nexus antes de ejecutar. El runtime no
debe tener tools directas para approve, reject o writes sensibles sin gate.
Cuando Axis necesita consumir un servicio externo, esa salida debe vivir como
adapter tecnico en codigo del dominio correspondiente.

## Capability manifests

Cada capability se normaliza a `capability_manifest.v1` antes de exponerse al
LLM o enviarse a Nexus. El manifest canónico incluye:

- identidad versionada: `capability_id`, `version`, `owner`, `product_surface`,
  y metadatos del producto;
- contrato: `input_schema`, `output_schema`, `evidence_schema`,
  `required_evidence`, preconditions y postconditions;
- control operativo: `action_type`, `risk_level`, `side_effect_type`,
  `auth_mode`, scopes, timeout, retries, cost/rate class;
- control de acciones sensibles: `nexus_action_type`, `approval_required`,
  idempotency mode y rollback/compensation strategy.

El loader de manifests nuevos es estricto: schemas con `required` apuntando a
propiedades inexistentes se rechazan.

Endpoint operativo:

- `POST /v1/capabilities`: importa manualmente un manifest como `draft`.
- `POST /v1/capabilities/import-source`: importa manifests desde una fuente JSON
  generica como `draft`.
- `POST /v1/capabilities/{capability_id}/versions/{version}/promote`: activa un
  manifest solo si pasa conformance.
- `POST /v1/capabilities/{capability_id}/versions/{version}/deprecate`: marca
  una version como no recomendada para nuevas ejecuciones.
- `POST /v1/capabilities/{capability_id}/versions/{version}/block`: bloquea un
  manifest manualmente.

Invariantes:

- Companion decide qué capability quiere usar, no decide el resultado final de
  acciones críticas.
- Los writes deben declarar `approval_required=true` y `nexus_action_type`.
- Los manifests deben declarar `required_scopes`, version semver,
  `evidence_schema` con propiedades y compatibilidad entre `action_type` y
  `side_effect_type`.
- Una capability con rollback automático debe declarar `rollback_capability_id`.
- La metadata que ve el planner y la metadata enviada a Nexus derivan del mismo
  manifest versionado.
- Cada import conserva provenance: `source`, `source_uri` cuando aplica,
  `imported_by`, `created_at` como instante de import y `updated_at`.
- `promote`, `deprecate` y `block` registran una conformance/audit run con
  transicion de estado, actor y `impact=unknown` hasta persistir consumers.
- Una version `blocked` no puede volver a `active` con promote directo; debe
  reimportarse o pasar por un flujo explicito futuro de unblock auditado.
- Una version `deprecated` puede reactivarse con promote si vuelve a pasar
  conformance; esa reactivacion queda auditada.
- Si existe runtime control plane y falta una política de customer org, la
  ejecución de capabilities falla cerrado.
