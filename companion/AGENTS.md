# Agents

Companion usa un control plane con perfiles seedables en `internal/agents` y
enforcement en `internal/runtime`.

## Modelo actual

Cada run produce:

- `IdentityChain`: usuario, tenant, product surface, scopes y principal
  `companion.employee_ai`.
- `AgentRoute`: intención clasificada, producto, autonomía efectiva y allowed
  tools.
- `AgentProfile`: perfil efectivo versionado, autonomía máxima, allowlist de
  tools, memory policy y scopes requeridos.

El routing sigue siendo determinístico y simple. El registry actual es seedable
en código; persistencia dinámica por producto queda fuera de esta iteración.

## Autonomía

Niveles soportados: `A0` a `A5`. Default: `A2`. La autonomía no reemplaza a
Nexus: una acción sensible sigue requiriendo governance aunque el agent tenga
mayor autonomía.

## Próxima evolución

Evolución pendiente:

- Persistir perfiles editables por producto/tenant.
- Agregar handoff humano y rollout por versión de perfil.
