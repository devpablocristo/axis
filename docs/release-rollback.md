# Axis Release And Rollback

Axis es un monorepo con deployables independientes. Un deploy de `Companion`,
`Nexus`, `BFF` o `Console` no implica redeploy de los otros servicios.

## CI Gates

`Axis CI` corre en PR, `develop`, `main`, manual y nightly:

- tests Go de `companion`, `nexus` y `bff`;
- build/typecheck de `console`;
- hygiene, Docker Compose config y whitespace;
- migraciones;
- OpenAPI;
- onboarding genérico `reference`;
- smoke MCP crítico en PR/push;
- smoke completo `make smoke` solo en nightly/manual.

Los jobs que deberían ser branch protection mínimos son:

- `api-contracts`;
- `companion`;
- `nexus`;
- `bff`;
- `console`;
- `hygiene`;
- `mcp-smoke`.

`platform-nightly` no debería bloquear PR: cubre flujos pesados y deja
artefactos de diagnóstico si falla.

## Deploy DEV

Workflows independientes:

- `Deploy Companion DEV`: `.github/workflows/deploy-companion-dev.yml`;
- `Deploy Nexus DEV`: `.github/workflows/deploy-nexus-dev.yml`;
- `Deploy BFF DEV`: `.github/workflows/deploy-bff-dev.yml`;
- `Deploy Console DEV`: `.github/workflows/deploy-console-dev.yml`.

Cada workflow acepta `workflow_dispatch.inputs.ref`. Para deployar un SHA o tag
concreto:

1. Abrir el workflow del servicio.
2. Ejecutar `Run workflow`.
3. Pasar `ref=<sha|tag|branch>`.
4. Confirmar que el smoke check del workflow queda verde.

## Rollback

Rollback recomendado:

1. Identificar el último SHA sano del servicio afectado.
2. Ejecutar manualmente el workflow DEV del servicio con `ref=<sha-sano>`.
3. Verificar `/readyz`.
4. Revisar logs de Cloud Run y errores de dependencias.
5. Si el servicio consume otro servicio Axis, confirmar URLs y audiencias JWT.

Cloud Run también permite volver a enviar tráfico a una revisión anterior, pero
el flujo preferido es redeployar el SHA sano para que GitHub Actions deje rastro
auditable.

## Service Notes

`Companion`:

- Requiere base de datos, Nexus base URL, API keys e internal JWT secret.
- Para exponer Ponti como producto Axis, requiere `PONTI_BASE_URL` y la secret
  `PONTI_API_KEY` con el mismo valor que `PONTI_AXIS_API_KEY` en Ponti.
- Sus watchers pueden apagarse con intervalos `0` durante incidentes.
- Si el conector Ponti queda sin manifest luego de un rollback o deploy, correr
  `POST /v1/connectors/refresh` y luego el smoke Ponti read-only.
- Validar `mcp-smoke` y `platform-nightly` si el cambio tocó MCP, runtime,
  observability, products, capabilities o Nexus integration.

`Nexus`:

- Es el plano determinístico de aprobación.
- Rollback de policies productivas debe usar endpoints de policy promotion y no
  edición directa, salvo break-glass controlado.
- Validar `smoke-nexus` antes de declarar sano un rollback.

`BFF`:

- Depende de URLs de Companion/Nexus y audiencias JWT.
- Si falla después de deploy, revisar primero `AXIS_COMPANION_BASE_URL_DEV`,
  `AXIS_NEXUS_BASE_URL_DEV` y `AXIS_INTERNAL_JWT_SECRET`.

`Console`:

- Depende de `AXIS_BFF_BASE_URL`.
- Rollback suele ser seguro si el contrato BFF no cambió. Si el contrato cambió,
  rollback debe coordinarse con BFF.

## Diagnostics

Los jobs de smoke suben artefactos en fallo:

- `mcp-smoke-diagnostics-<run_id>`;
- `platform-nightly-diagnostics-<run_id>`.

Los artefactos incluyen `docker compose ps` y logs de Companion, Nexus y sus
Postgres locales.
