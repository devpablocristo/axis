# Contracts

Contratos compartidos entre `companion`, `nexus`, `bff` y `console`.

La regla es compartir contratos, no imports directos a internals de servicios.

- `companion-admin.ts`: DTOs TypeScript mínimos para superficies admin de
  Companion usadas por Console vía BFF. No contiene lógica interna.
- `product-integration.ts`: contrato v1 para conectar N productos a Axis:
  identidad `org_id + product_surface`, installations, workspace opaco y
  referencias a capability manifests.
