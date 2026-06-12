# Auth

Helpers compartidos de identidad interna, scopes, tenants y token exchange.

Los servicios productivos siguen validando identidad en sus propios boundaries.

## Product JWT

Companion y Nexus aceptan JWTs HS256 por producto, ademas del JWT interno de
plataforma y los API keys. Config por env:

- `COMPANION_PRODUCT_JWT_KEYS="ponti=<secret>|issuer=ponti-core"`
- `NEXUS_PRODUCT_JWT_KEYS="ponti=<secret>|issuer=ponti-core"`

Entries separados por `;`, `,` o newline; cada entry es
`product=<secret>|issuer=<issuer>`.

Claims del token: `iss` (issuer del producto), `aud` (servicio receptor:
`companion`/`nexus`), `sub`/`actor_id`, `org_id`, `product_surface`,
`scope`/`scopes`, `service_principal`, `on_behalf_of`, `exp` corto.

Los principals resultantes llevan `AuthMethod=product_jwt`. No equivalen a
`api_key`: en Nexus, la delegacion de `decided_by` en approvals sigue
restringida a service principals autenticados por API key. Contrato completo
en `docs/companion/security.md` y
`docs/companion/product-integration-contract-v1.md`.
