# Modelo IAM Axis + Clerk

## Regla simple

Clerk administra las cuentas humanas que entran a Axis Console. No modela la
jerarquia completa de negocio de los productos conectados.

Clerk usa este modelo:

```text
Clerk Application -> Organizations -> Members/Roles
```

Como Clerk no tiene organizaciones anidadas, Axis no debe representar
`Acme -> Willy.inc -> Qwerty` como tres niveles de `Organization` dentro de la
app Clerk de Axis.

## Nombres canonicos en Axis

| Concepto | Ejemplo | Donde vive |
| --- | --- | --- |
| Axis owner | cristo.tech / axis_role=owner | Clerk user metadata + Axis |
| Cuenta Axis | Acme | Clerk Organization + mirror Axis |
| Producto | Willy.inc | Axis DB / product registry |
| Tenant del producto | Qwerty | Axis DB / contrato del producto |
| Usuario externo | usuario de Qwerty | Producto o IdP del producto |

## UI

IAM debe mostrar solo identidades humanas que operan Axis:

- Equipo Axis
- Cuentas Axis
- Usuarios de cuenta

Productos debe mostrar el dominio conectado:

- Productos
- Tenants
- Usuarios externos

La barra operativa usa:

```text
Cuenta Axis: Acme
Producto: Willy.inc
Tenant: Qwerty
```

## Contrato runtime

Companion y Nexus mantienen el contrato actual:

- `org_id`: cuenta Axis / customer account.
- `product_surface`: producto conectado.
- `external_tenant_id`: tenant nativo del producto conectado.

Los usuarios de `Qwerty` no son usuarios IAM de Axis salvo que entren a Axis
Console. Si necesitan login propio, lo administra `Willy.inc` o su IdP.
