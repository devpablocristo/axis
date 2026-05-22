# Axis BFF

Backend-for-frontend para `../console`.

Responsabilidades:

- Validar sesión humana con OIDC (`AXIS_BFF_AUTH_MODE=oidc`) o modo dev local.
- Resolver el `org_id` efectivo.
- Firmar Bearer JWT interno para `companion` y `nexus`.
- Exponer `/api/companion/*`, `/api/nexus/*`, `/api/session`,
  `/api/services`, `/healthz` y `/readyz`.

El browser nunca llama directo a `companion` ni `nexus`.
