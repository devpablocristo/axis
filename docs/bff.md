# Axis BFF

Backend-for-frontend para `../console`.

Responsabilidades:

- Validar sesión humana con OIDC (`AXIS_BFF_AUTH_MODE=oidc`) o modo dev local.
- Resolver el `org_id` efectivo.
- Firmar Bearer JWT interno para `companion` y `nexus`.
- Exponer `/api/companion/*`, `/api/nexus/*`, `/api/session`,
  `/api/services`, `/healthz` y `/readyz`.

El browser nunca llama directo a `companion` ni `nexus`.

Para el modelo IAM con Clerk y la separacion entre Cuenta Axis, Producto,
Tenant y usuarios externos, ver `docs/iam-axis-clerk-model.md`.

## Identidad humana

- El BFF usa un puerto interno `HumanIdentityProvider` para IAM humano.
- Los handlers trabajan contra ese puerto y contra el store local `axis_*`.
- Clerk vive sólo como adapter de infraestructura (`identity_clerk.go`) y en la
  composición/configuración del BFF.
- La DB local de Axis queda como mirror operativo/audit; los permisos efectivos
  se calculan en el BFF, no se confían desde el browser.
