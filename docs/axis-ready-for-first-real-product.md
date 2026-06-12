# Axis Ready For First Real Product

Estado al 2026-06-07: Axis queda listo para iniciar la integracion del primer
producto real solo si este checklist pasa en `develop`.

## Gate Ejecutable

```bash
cd companion
bash scripts/onboarding/check-axis-readiness.sh
```

El gate valida dos productos fake:

- `reference`;
- `shadow`.

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
errors y eval pack con tenant leakage max `0`.

## Criterios De Readiness

- No hay hardcode vertical requerido para registrar un producto nuevo.
- `org_id + product_surface` es el scope operativo.
- Un producto externo sin installation activa falla cerrado.
- Read capabilities tienen evidence schema y scopes.
- Writes o side effects requieren `nexus_action_type` y
  `approval_required=true`.
- Evals incluyen routing, evidence, tenant leakage y action safety.
- Ops puede observar producto por metrics, alerts, SLOs, costs, traces y jobs.
- MCP sigue siendo capa operativa sobre APIs Axis y pasa por runtime policy +
  Nexus.

## No Hacer Antes Del Primer Producto Real

- No agregar reglas Ponti/Pymes dentro de Companion.
- No saltar `Product Integration Contract v1`.
- No crear writes sin Nexus.
- No activar self-service onboarding externo.
- No automatizar retention destructiva sin dry-run y aprobacion operativa.

## Siguiente Paso

El siguiente trabajo puede ser planear Ponti como consumidor, usando los mismos
contratos fake ya validados. La integracion debe empezar read-only y con feature
flag.
