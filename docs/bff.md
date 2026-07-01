# Axis BFF

Backend-for-frontend para `../console`.

Responsabilidades:

- Validar sesión humana con OIDC (`AXIS_BFF_AUTH_MODE=oidc`) o modo dev local.
- Resolver el `org_id` efectivo.
- Firmar Bearer JWT interno para `companion` y `nexus`.
- Exponer `/api/companion/*`, `/api/nexus/*`, `/api/session`,
  `/api/services`, `/healthz` y `/readyz`.
- Exponer superficies Console especificas como `/api/virployees`,
  `/api/agent-profiles` y `/api/job-roles` como proxies acotados a Companion.

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

## Job Roles

`/api/job-roles` es la superficie BFF para administrar puestos de trabajo de
Virployees desde Console. El BFF no implementa dominio propio: resuelve
`org_id + product_surface`, firma el token interno y forwardea a
`/v1/job-roles` en Companion.

JobRole no es IAM Role ni PermissionBundle. En v1 reutiliza scopes operativos
de Agents y no autoriza acciones.

## Connectors

`/api/connectors` es la superficie BFF para administrar connectors desde
Console. El BFF no define schemas de configuracion: forwardea a Companion y
Console renderiza formularios desde `/api/connectors/types`.
