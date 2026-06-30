# Companion Domain Specs

Este directorio contiene specs de dominio objetivo para Companion.

La diferencia con `docs/companion/*.md` es intencional:

- `docs/companion/*.md` describe conceptos, operacion y contratos actuales.
- `docs/specs/companion/domain/*.md` fija modelos objetivo de dominio.

Un spec de dominio no implica que el codigo actual ya implemente ese modelo.
Debe separar explicitamente:

- modelo objetivo;
- implementacion actual;
- decisiones de naming;
- entidades referenciadas;
- campos que no pertenecen al core.

## Specs

- `workforce-domain-spec.md`: mapa rector del dominio Workforce.
- `identity-and-tenancy-domain-spec.md`: `Organization`, `Product`, `Tenant`
  y `User`.
- `virtual-employees-domain-spec.md`: modelo objetivo core de
  `VirtualEmployee`.
- `job-roles-domain-spec.md`: `JobRole`, `Responsibility` y
  `SuccessCriterion`.
- `employee-profiles-domain-spec.md`: `EmployeeProfile`, `LLMConfig` y
  `MemoryPolicy`.
- `capabilities-and-tools-domain-spec.md`: `Capability` y `Tool`.
- `memory-domain-spec.md`: `Memory` y `MemoryEntry`.
- `agents-domain-spec.md`: `Agent` como ejecutor tecnico, separado de
  `VirtualEmployee`.
- `work-domain-spec.md`: `Task`, `Watcher` y `Handoff`.
- `audit-domain-spec.md`: `AuditEvent` y reglas de historial.

## Reglas

- Una entidad fuerte se referencia por ID.
- Un value object vive embebido solo si no tiene lifecycle propio.
- Un dato derivado no se duplica como campo persistido.
- Audit completo vive en eventos o tabla de audit, no en el core de dominio.
- Specs de dominio no definen migraciones, endpoints ni pantallas salvo que sea
  necesario para aclarar una frontera conceptual.
- Un spec debe bajar cada entidad y value object hasta primitivas, enums o
  referencias por ID.
- Campos flexibles no pueden quedar como `metadata`, `json`, `object`,
  `config` o `payload` sin un value object versionado que explique su forma.
