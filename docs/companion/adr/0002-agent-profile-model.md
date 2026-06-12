# ADR 0002: Agent Profile Model

## Estado

Propuesto para Sprint 0 de la migracion `pymes/ai` -> Companion.

## Decision

Companion carga perfiles versionados e inmutables. Un perfil define identidad,
prompt, permisos de tools/capabilities, configuracion LLM y scope de memoria.
Los productos publican capabilities; Companion arma perfiles que seleccionan un
subconjunto permitido de esas capabilities.

## Perfil canonico

Campos requeridos:

- `id`: estable, por ejemplo `pymes.bike_shop.operator`.
- `version`: semver, immutable.
- `product`: `pymes`, `nexus`, `ponti`, etc.
- `system_prompt`: texto versionado con variables declaradas.
- `allowed_tools`: tools internas de Companion.
- `allowed_capabilities`: IDs publicados por productos.
- `llm_config`: modelo, temperatura y limites.
- `memory_scope`: `per_actor`, `per_tenant` o `shared`.
- `required_scopes`: scopes necesarios para usar el perfil.

## Reglas

- Una conversacion conserva la version de perfil con la que arranco.
- Cambiar prompt/capabilities exige nueva version.
- Si el perfil referencia una capability inexistente, Companion falla al boot o
  marca el perfil unavailable; no degrada silenciosamente.
- Companion no autoriza negocio. Cada capability call vuelve al producto, y el
  producto reautoriza tenant, actor, rol y permisos.

## Perfiles iniciales Pymes

- `pymes.bike_shop.operator.v1`
- `pymes.auto_repair.operator.v1`
- `pymes.teachers.operator.v1`

## Fuera de alcance

- Autoedicion de perfiles por LLM.
- Tools genericas tipo SQL/HTTP arbitrary execution.
- Un modo publico interno magico; el chat publico entra por gateway o BFF.
