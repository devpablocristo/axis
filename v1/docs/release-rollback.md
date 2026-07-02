# Axis Release And Rollback

Axis es un monorepo con deployables independientes. Un deploy de `Companion`,
`Nexus`, `BFF` o `Console` no implica redeploy de los otros servicios.

Este runbook describe los workflows actuales de GitHub Flow: PR contra `main`,
deploy automatico a STG al pushear `main`, preview por PR y deploy PRD manual.

## CI Gates

`Axis CI` corre en PR contra `main`, push a `main`, manual y nightly:

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

## Deploy STG / PRD

Workflows:

- `Deploy STG`: `.github/workflows/deploy-stg.yml`, automatico en push a
  `main` cuando cambian deployables o Docker/workflows, y manual con
  `ref`/`product`. Usa los servicios actuales `axis-nexus`,
  `axis-companion`, `axis-bff` y `axis-console`.
- `Deploy PRD`: `.github/workflows/deploy-prd.yml`, manual con GitHub
  environment `prd`.
- `Preview PR`: `.github/workflows/preview-pr.yml`, automatico para PRs a
  `main` desde el mismo repo cuando cambian deployables o workflows.
- `Preview Deploy` y `Preview Cleanup`: workflows reutilizables para crear y
  eliminar previews efimeras por PR.
- `Preview Audit`: workflow de mantenimiento para revisar previews vivas.

Para deployar un SHA o tag concreto:

1. Abrir `Deploy STG` o `Deploy PRD`.
2. Ejecutar `Run workflow`.
3. Pasar `ref=<sha|tag|branch>` y `product=<all|nexus|companion|bff|console>`.
4. Confirmar que el smoke check del workflow queda verde.

Notas de entorno:

- STG usa variables `*_STG` con defaults seguros para el proyecto Axis STG. La
  base Cloud SQL puede apuntar a la instancia compartida existente definida por
  `CLOUDSQL_INSTANCE_STG`; los workflows no crean instancias.
- PRD no tiene defaults de proyecto/identidad: requiere variables `*_PRD` y el
  environment `prd`.
- `AXIS_BFF_AUTH_MODE_*` controla la autenticacion del BFF. Si el modo es
  `oidc`, `AXIS_AUTH_ISSUER_URL_*` y `AXIS_AUTH_AUDIENCE_*` son obligatorios.
- No registrar valores de secrets. Documentar nombres de Secret Manager o
  variables, nunca valores reales.

## Rollback

Rollback recomendado:

1. Identificar el último SHA sano del servicio afectado.
2. Ejecutar manualmente `Deploy STG` o `Deploy PRD` con `ref=<sha-sano>` y el
   `product` afectado.
3. Verificar `/readyz`.
4. Revisar logs de Cloud Run y errores de dependencias.
5. Si el servicio consume otro servicio Axis, confirmar URLs y audiencias JWT.

Cloud Run también permite volver a enviar tráfico a una revisión anterior, pero
el flujo preferido es redeployar el SHA sano para que GitHub Actions deje rastro
auditable.

## Service Notes

`Companion`:

- Requiere base de datos, Nexus base URL, API keys e internal JWT secret.
- Para productos externos, requiere una installation activa
  `org_id + product_surface`, manifests conformes y referencias de secrets.
- Sus watchers pueden apagarse con intervalos `0` durante incidentes.
- Validar `mcp-smoke` y `platform-nightly` si el cambio tocó MCP, runtime,
  observability, products, capabilities o Nexus integration.

`Nexus`:

- Es el plano determinístico de aprobación.
- No debe incorporar runtime LLM, memoria IA ni agentes.
- Rollback de policies productivas debe usar endpoints de policy promotion y no
  edición directa, salvo break-glass controlado.
- Validar `smoke-nexus` antes de declarar sano un rollback.

`BFF`:

- Depende de URLs de Companion/Nexus y audiencias JWT.
- Si falla después de deploy, revisar primero las URLs resueltas de
  Companion/Nexus, `AXIS_INTERNAL_JWT_SECRET` y el modo
  `AXIS_BFF_AUTH_MODE_*`.
- Cuando el principal tiene scopes cross-org, puede seleccionar la organizacion
  efectiva con `X-Axis-Org-ID` y firma un JWT interno acotado a esa org.

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

## Limpieza De Preview

Cuando una PR se cierra, `Preview Cleanup` debe eliminar servicios Cloud Run,
imagenes o recursos efimeros asociados al numero de PR. Si una preview queda
viva por una cancelacion manual o un fallo de GitHub Actions, ejecutar
manualmente `Preview Audit` y luego `Preview Cleanup` para el PR afectado.
