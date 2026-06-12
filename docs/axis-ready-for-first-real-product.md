# Axis Ready For First Real Product

Estado al 2026-06-12: Axis mantiene un gate ejecutable para demostrar que puede
conectar productos reales sin hardcode vertical en Companion, Nexus ni BFF. Este
documento conserva el checklist para cualquier producto nuevo y para cambios que
toquen runtime, products, capabilities, observability, memory, jobs o Nexus
integration.

## Gate Ejecutable

```bash
cd companion
bash scripts/onboarding/check-axis-readiness.sh
```

El gate valida dos productos fake:

- `reference`;
- `shadow`.

Los fixtures fake son obligatorios porque prueban que Axis no depende de
defaults Ponti/Pymes/Medmory ni de conocimiento vertical de un producto real.

Para validar contratos reales sin convertirlos en default del gate base:

```bash
cd companion
AXIS_REAL_PRODUCTS=ponti,medmory bash scripts/onboarding/check-axis-readiness.sh
```

Esto agrega `scripts/onboarding/ponti-product-contract.json` y
`scripts/onboarding/medmory-product-contract.json` al mismo checklist. Los
fixtures `reference`/`shadow` siguen siendo obligatorios y no pueden usar
surfaces reales.

Cada producto tiene contract spec, installation activa, identity/JWT context,
read capability, write/draft capability gobernada por Nexus metadata, expected
errors y eval pack con cross-org/product leakage max `0`.

## Criterios De Readiness

- No hay hardcode vertical requerido para registrar un producto nuevo.
- `org_id + product_surface` es el scope operativo.
- Un producto externo sin installation activa falla cerrado.
- Read capabilities tienen evidence schema y scopes.
- Writes o side effects requieren `nexus_action_type` y
  `approval_required=true`.
- Evals incluyen routing, evidence, cross-org/product leakage y action safety.
- Ops puede observar producto por metrics, alerts, SLOs, costs, traces y jobs.
- MCP sigue siendo capa operativa sobre APIs Axis y pasa por runtime policy +
  Nexus.
- Jobs, memory, observability y costs transportan `product_surface` como campo
  operativo, no como metadata opcional.

## No Hacer En Onboarding

- No agregar logica vertical de ningun producto dentro de Companion o Nexus.
- No saltar `Product Integration Contract v1`.
- No crear writes sin Nexus.
- No activar self-service onboarding externo.
- No automatizar retention destructiva sin dry-run y aprobacion operativa.
- No usar `tenant` como concepto nuevo en Companion; `org_id` representa la
  customer org y `tenant` queda como compatibilidad historica en nombres ya
  existentes.

## Uso Para Un Producto Nuevo

1. Crear `scripts/onboarding/<product>-product-contract.json`.
2. Crear `scripts/evals/<product>-golden.json`.
3. Ejecutar `AXIS_REAL_PRODUCTS=<product> bash scripts/onboarding/check-axis-readiness.sh`.
4. Empezar read-only con installation activa y feature flag.
5. Agregar writes solo con metadata Nexus, evidence y approval policy.

Ponti y Medmory son fixtures/consumidores reales de referencia. No son defaults
del runtime compartido.
