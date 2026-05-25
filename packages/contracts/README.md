# Contracts

Contratos compartidos entre `companion`, `nexus`, `bff` y `console`.

La regla es compartir contratos, no imports directos a internals de servicios.

- `companion-admin.ts`: DTOs TypeScript mínimos para superficies admin de
  Companion usadas por Console vía BFF. No contiene lógica interna.
