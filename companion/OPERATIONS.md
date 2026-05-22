# Operations

## Local

```bash
test -f .env || cp .env.example .env
make up
make logs
```

Nexus Governance debe estar levantado aparte y accesible por
`GOVERNANCE_BASE_URL`.

Companion es headless: `make up` levanta solo API + Postgres. La UI operativa
futura vive fuera de este servicio y debe consumirlo vía BFF/HTTP.

Los connectors de ejecución son tenant-owned. El registry publica templates de
capabilities, pero cada tenant debe crear su fila `/v1/connectors` con
`X-Org-ID`; no hay default operativo a `org_id=''`.

## Health

- `GET /healthz`: proceso vivo.
- `GET /readyz`: DB disponible.

## Migrations

El backend aplica migraciones embebidas al arrancar. Validar versiones con:

```bash
bash scripts/quality/check-migrations.sh
```

## Background loops

- `COMPANION_GOVERNANCE_SYNC_INTERVAL_SEC`: sync de tasks con Nexus.
- `COMPANION_STRICT_GOVERNANCE`: activa fail-closed estricto para grants Nexus.
- `COMPANION_WATCHER_INTERVAL_SEC`: ejecución periódica de watchers.
- `COMPANION_WATCHER_SYNC_INTERVAL_SEC`: reconciliación de watcher proposals.
- Memory purge corre cada hora.

## Smoke

```bash
make smoke
```

Los smoke scripts esperan Companion y Nexus levantados, y usan las keys de
`.env`.

## GCP DEV deploy (Cloud Run)

Companion-dev se despliega vía
`.github/workflows/deploy-companion-dev.yml`. Modelo: WIF para auth,
Artifact Registry para la imagen, Cloud SQL vía socket, Secret Manager
para credenciales, Cloud Run con `--no-allow-unauthenticated` y
`--ingress=internal-and-cloud-load-balancing`. **Solo DEV**, branch
`develop`.

### Infra GCP ya aprovisionada (vía gcloud)

Project: `pymes-dev-352318` · Region: `us-central1` · Branch: `develop`

Instancia Cloud SQL **compartida** entre los tres productos del proyecto:

| Producto | DB | DB user |
|---|---|---|
| Pymes (varios) | pre-existente | varios |
| Nexus governance | `nexus` | `governance_app` |
| Companion | `companion` | `companion_app` |

| Recurso | Nombre | Notas |
|---|---|---|
| Cloud SQL instance | `pymes-dev-db` | reusada; Postgres 16; shared |
| DB | `companion` | creada |
| DB user | `companion_app` | password en Secret Manager |
| Artifact Registry repo | `pymes` (us-central1) | reusado; imagen `companion:<sha>` |
| Runtime SA | `companion-runtime-dev@pymes-dev-352318.iam.gserviceaccount.com` | roles: `roles/cloudsql.client`, `roles/secretmanager.secretAccessor` sobre los 4 secrets |
| Deploy SA (WIF) | `github-actions@pymes-dev-352318.iam.gserviceaccount.com` | ya tenía `run.admin`, `artifactregistry.writer`, `cloudsql.client`, `secretmanager.secretAccessor`; agregado `iam.serviceAccountUser` sobre la runtime SA |
| WIF pool / provider | `github-actions-pool` / `github-actions-provider` | `attributeCondition`: `(repo=='devpablocristo/pymes' \|\| repo=='devpablocristo/companion' \|\| repo=='devpablocristo/nexus') && ref=='refs/heads/develop'`; binding agregado para los 3 repos |

Instancia previa de Nexus (`nexus-db-dev` en proyecto `new-nexus-dev`) fue
**apagada** (`activationPolicy=NEVER`); pendiente borrar definitivamente
tras verificar que la DB `pymes` que tiene adentro es chatarra.

Secrets en Secret Manager (`companion-runtime-dev` tiene `secretAccessor`):

- `companion-db-password` — real, generada al crear el user `companion_app`.
- `companion-api-keys` — generada con `ADMIN_KEY` (64 hex) y un `org_id` UUID. Guardar en lugar seguro para usar contra el endpoint admin.
- `companion-governance-api-key` — **placeholder `REPLACE_ME_AFTER_NEXUS_DEPLOY`**; rotar cuando Nexus esté arriba.
### Variables que faltan settear en GitHub repo (`devpablocristo/companion` → Settings → Variables → Actions)

```text
GCP_PROJECT_ID_DEV = pymes-dev-352318
GCP_REGION = us-central1
WIF_PROVIDER_DEV = projects/884236221349/locations/global/workloadIdentityPools/github-actions-pool/providers/github-actions-provider
WIF_SERVICE_ACCOUNT_DEV = github-actions@pymes-dev-352318.iam.gserviceaccount.com
ARTIFACT_REGISTRY = pymes
CLOUDSQL_INSTANCE_DEV = pymes-dev-352318:us-central1:pymes-dev-db
COMPANION_CLOUD_RUN_SERVICE_ACCOUNT_DEV = companion-runtime-dev@pymes-dev-352318.iam.gserviceaccount.com
COMPANION_GOVERNANCE_BASE_URL_DEV = <pending — completar cuando Nexus se deploye>
COMPANION_LLM_PROVIDER_DEV = vertex
COMPANION_LLM_MODEL_DEV = gemini-2.5-flash-lite
COMPANION_LLM_VERTEX_PROJECT_DEV = pymes-dev-352318
COMPANION_LLM_VERTEX_LOCATION_DEV = us-central1
```

### Dependencia bloqueante: Nexus governance no está deployado

`cmd/api/main.go` aborta si `GOVERNANCE_BASE_URL` o `GOVERNANCE_API_KEY`
están vacíos. Sin Nexus en GCP, el primer deploy se va a estrellar en el
smoke check. Dos caminos para destrabar:

1. Deployar Nexus governance al mismo proyecto y completar
   `COMPANION_GOVERNANCE_BASE_URL_DEV` + rotar `companion-governance-api-key`.
2. Apuntar temporalmente a una instancia externa de Nexus (ngrok, otra
   cloud) — solo para validar el pipeline.

### Disparar el deploy

- Automático en push a `develop` que toque `cmd/`, `internal/`, `wire/`,
  `migrations/`, `go.mod`, `go.sum`, `Dockerfile` o el workflow.
- Manual: GitHub Actions → "Deploy Companion DEV" → Run workflow → `ref`.

### Verificación post-deploy

Cloud Run rechaza tráfico anónimo. Para curl manual desde tu máquina:

```bash
TOKEN="$(gcloud auth print-identity-token)"
URL="$(gcloud run services describe companion-dev \
  --project="pymes-dev-352318" --region="us-central1" \
  --format='value(status.url)')"
curl -H "Authorization: Bearer $TOKEN" "$URL/readyz"
```

Tu identidad necesita `roles/run.invoker` sobre `companion-dev`.
