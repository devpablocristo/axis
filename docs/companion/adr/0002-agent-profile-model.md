# ADR 0002: Agent Profile Model

## Estado

Implementado en v1 backend/API.

## Decision

Companion carga perfiles globales versionados desde Postgres. Un perfil define
prompt, autonomia maxima, permisos opcionales de tools/capabilities,
configuracion LLM y politica de memoria. Los productos publican capabilities;
Agent Fleet instala agentes por `org_id + product_surface` y referencia el
perfil global mediante `profile_id`.

V1 no tiene overrides por org/producto. El allowlist product-specific vive en
`companion_agents.allowed_capabilities`; el perfil global puede acotar mas, pero
no amplia permisos del agente instalado.

## Perfil canonico

Campos requeridos:

- `profile_id`: estable, por ejemplo `axis.ops.billing.v1`.
- `family_id`: familia versionable, por ejemplo `axis.ops.billing`.
- `version_label`: etiqueta versionada, por ejemplo `v1`.
- `system_prompt`: texto versionado con variables declaradas.
- `allowed_tools`: tools internas de Companion.
- `allowed_capabilities`: IDs publicados por productos.
- `llm_config`: modelo, temperatura y limites.
- `memory_scope`: `per_actor`, `per_tenant` o `shared`.
- `required_scopes`: scopes necesarios para usar el perfil.

## Reglas

- Cada update de perfil guarda snapshot previo en `agent_profile_versions`.
- Si un agente referencia un perfil inexistente, disabled o archived, el runtime
  falla cerrado antes de llamar al LLM.
- `max_autonomy` del perfil solo puede reducir la autonomia efectiva.
- Companion no autoriza negocio. Cada capability call vuelve al producto, y el
  producto reautoriza tenant, actor, rol y permisos.

## Perfil inicial

- `axis.ops.billing.v1`: agente agnostico de billing. Medmory lo usa mediante
  `billing_agent`, pero el prompt no menciona Medmory ni datos clinicos como
  flujo central.

## Diferencia con Assist Packs

- `assist_packs`: prompts de productos y casos de uso, por `org_id`,
  `owner_system`, `product_surface` y `assist_type`.
- `agent_profiles`: prompts de agentes Axis, globales, versionados y referidos
  por `companion_agents.profile_id`.

## Fuera de alcance

- Autoedicion de perfiles por LLM.
- Tools genericas tipo SQL/HTTP arbitrary execution.
- Un modo publico interno magico; el chat publico entra por gateway o BFF.
- UI de edicion de perfiles y overrides por org/producto.
