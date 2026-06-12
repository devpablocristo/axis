# Product Integration Contract v1

Este contrato define como cualquier producto propio se conecta a Axis sin
hardcode vertical.

## Conceptos

| Campo | Significado |
|---|---|
| `org_id` | Cliente/organizacion final. Es el tenant real de Axis. |
| `product_surface` | Producto conectado: `ponti`, `pymes`, `argos`, etc. |
| `product_installation` | Instalacion activa de `org_id + product_surface`. |
| `external_tenant_id` | Tenant/id nativo del producto conectado. |
| `workspace` | JSON opaco del producto. Axis lo transporta, no lo interpreta. |

`companion` es una superficie interna de Axis. No requiere una
`product_installation` externa. Cualquier `product_surface != companion` debe
resolver una instalacion activa para `org_id + product_surface` antes de usar
runtime, capabilities, connector executions, watchers o memory writes.

## Identity Context

Payload minimo esperado desde producto hacia Axis:

```json
{
  "org_id": "org-123",
  "product_surface": "ponti",
  "actor_id": "user-456",
  "actor_type": "human",
  "on_behalf_of": "user-456",
  "service_principal": false,
  "external_tenant_id": "ponti-tenant-789",
  "scopes": ["companion:tasks:write"],
  "workspace": {
    "project_id": 10,
    "campaign_id": 3
  }
}
```

Headers canonicos:

- `Authorization: Bearer <internal-jwt>` o `X-API-Key`.
- `X-Product-Surface: <product_surface>` cuando el token no incluya el claim.
- `X-On-Behalf-Of: <actor_id>` para llamadas service-to-service delegadas.

Claims minimos del JWT interno:

- `org_id`
- `actor_id`
- `actor_type`
- `product_surface`
- `service_principal`
- `on_behalf_of`
- `scopes`
- `iss`: issuer interno de Axis.
- `aud`: audiencia esperada por el servicio Axis receptor.
- `exp`: expiracion corta obligatoria.
- `iat`: fecha de emision.
- `kid`: identificador de key para rotacion/JWKS cuando haya keyset
  versionado.

Politica inicial:

- Tokens service-to-service deben ser de vida corta y con `aud` del servicio
  destino.
- `kid` es obligatorio cuando la key no sea estatica de dev.
- La rotacion operativa mantiene al menos una key activa y una key anterior en
  periodo de gracia.
- Ningun producto debe enviar secretos en claims; solo referencias o scopes.

## Product Registry

Endpoints Companion:

- `GET /v1/products`
- `GET /v1/products/{product_surface}`
- `PUT /v1/products/{product_surface}`

Producto:

```json
{
  "product_surface": "ponti",
  "display_name": "Ponti",
  "status": "active",
  "metadata": {}
}
```

Reglas:

- `product_surface` debe ser estable, lowercase y no usar Ponti como default.
- `status` puede ser `active` o `disabled`.
- `metadata` no debe contener claves sensibles como `token`, `secret`,
  `api_key`, `password` o `authorization`.

## Product Installations

Endpoints Companion:

- `GET /v1/product-installations?org_id=<org>`
- `GET /v1/product-installations/{product_surface}?org_id=<org>`
- `PUT /v1/product-installations/{product_surface}?org_id=<org>`
- `GET /v1/product-installations/{product_surface}/resolve?org_id=<org>`

Instalacion:

```json
{
  "org_id": "org-a",
  "product_surface": "ponti",
  "external_tenant_id": "ponti-tenant-789",
  "base_url": "https://ponti.example.com",
  "auth_mode": "internal_jwt",
  "secret_ref": "",
  "enabled": true,
  "config": {
    "workspace_schema": "ponti.workspace.v1"
  }
}
```

Reglas:

- Resolver una instalacion exige `org_id + product_surface`.
- Sin instalacion activa, Axis falla cerrado.
- `ProductInstallationGuard` bloquea runtime runs, runtime capability tools,
  connector executions, watcher query/action, memory writes y jobs de memoria
  con superficie externa.
- Los bloqueos se registran como observability event
  `event_type=guardrail`, `event_name=product_installation_required`.
- `base_url` es obligatorio si `enabled=true`.
- `auth_mode` soporta `none`, `api_key_ref`, `oauth2`, `internal_jwt` y
  `custom`.
- `api_key_ref`, `oauth2` y `custom` requieren `secret_ref`.
- `secret_ref` debe usar una referencia segura con esquema:
  - `env:AXIS_PRODUCT_API_KEY` para local/dev;
  - `vault:axis/products/<product>/<org>/<name>` para adapter Vault futuro;
  - `secretmanager:projects/<project>/secrets/<name>` para adapter cloud
    futuro.
- `config` no puede contener secretos planos; se usan referencias seguras.
- El adapter local/dev solo resuelve `env:`. Los adapters productivos deben
  resolver `vault:` o `secretmanager:` sin exponer valores en APIs, logs ni
  observability.

## Capabilities

Cada producto expone capabilities mediante `capability_manifest.v1`.

Requisitos de conformance:

- `product_surface` debe existir y estar activo en el product registry cuando
  el usecase de capabilities tiene registry inyectado;
- `schema_version` debe ser `capability_manifest.v1` y `version` debe ser
  semver;
- schema valido;
- `required_scopes`;
- `risk_level`;
- `side_effect_type`;
- compatibilidad entre `action_type` y `side_effect_type`;
- `evidence_schema` con evidencia atribuible;
- metadata Nexus cuando una write capability requiere approval.

Una read capability puede exponerse al planner despues de pasar conformance.
Una write capability sin metadata Nexus debe rechazarse.

Estados persistidos:

- `draft`: importado pero no visible al runtime.
- `active`: visible para planner/runtime.
- `deprecated`: no recomendado para nuevas ejecuciones.
- `blocked`: bloqueado manualmente; no puede promoverse hasta ser reimportado o
  corregido.

Cada manifest conserva provenance operativo: `source`, `source_uri` cuando la
importacion viene de URL, `imported_by`, `created_at` como momento de import y
`updated_at`. Las transiciones `promote`, `deprecate` y `block` registran una
conformance/audit run con actor, cambio de estado e `impact=unknown`; Axis no
persiste todavia una relacion completa de consumers activos por capability.
Una version `deprecated` puede reactivarse con `promote` si vuelve a pasar
conformance. Una version `blocked` no se promueve directamente; debe reimportarse
o esperar un flujo explicito futuro de unblock auditado.

Import soportado:

- manual, enviando un `capability_manifest.v1`;
- source URL generica, enviando una fuente JSON que contenga un manifest unico o
  `{ "capabilities": [...] }`.

Axis no llama a productos reales automaticamente para descubrir capabilities.
La importacion por URL es una accion admin explicita y generica.

## Observabilidad, Costos y Evals

Axis atribuye runtime data por:

- `org_id`
- `product_surface`
- `agent_id`
- `capability_id`
- modelo LLM

Los eventos y costos pueden consultarse filtrando `product_surface`. Los eval
reports tambien quedan etiquetados por producto para separar packs como
`ponti-golden.json`, `pymes-golden.json` o futuros conjuntos.

El cost summary devuelve total por org/producto y desglose por producto,
capability, modelo y agente para atribuir consumo operativo.

Runtime policy soporta control por producto mediante:

- `allowed_product_surfaces`;
- `control_plane.product_policies[product_surface].denied`;
- `control_plane.product_policies[product_surface].max_autonomy`;
- `control_plane.product_policies[product_surface].monthly_cost_budget_cents`;
- `control_plane.product_policies[product_surface].monthly_tool_call_budget`.

Axis aplica rate limits por `org_id + product_surface` en areas separadas:

- runtime/chat;
- connector execution;
- watchers;
- eval runs.

Los jobs durables transportan `product_surface` como campo first-class. Si no se
informa, se asume `companion`; los jobs de producto deben setearlo
explicitamente para auditoria, budgets y rate limits.

El endpoint operativo `GET /v1/jobs` acepta `product_surface` como filtro. Los
principales sin `companion:cross_org` quedan limitados al producto declarado en
su identidad; `GET /v1/jobs/{id}` y cancelaciones validan `org_id +
product_surface` antes de exponer o modificar un job.

Las cuotas de memoria se calculan por `org_id + product_surface + scope_type +
scope_id`. Dos productos de la misma organizacion no comparten cuota ni pueden
bloquear inserciones entre si aunque reutilicen el mismo `scope_id`.

## Product Evals Y Contract Tests

## Readiness Freeze

Estado al 2026-06-07: `Product Integration Contract v1` queda congelado para
conectar el primer producto real. Cambios incompatibles requieren ADR y una
version `v2`; no deben hacerse como ajuste silencioso de Companion.

Compatibilidad que debe preservarse:

- `org_id` identifica la customer org real.
- `product_surface` identifica el producto conectado.
- toda superficie externa requiere `org_id + product_surface + installation`
  activa.
- `workspace` sigue siendo JSON opaco para Axis.
- manifests usan `capability_manifest.v1`.
- read capabilities pueden ejecutarse sin Nexus si pasan scopes/policy.
- write/side-effect capabilities requieren metadata Nexus y aprobación según
  policy.
- observability, costs, evals, jobs y memory se atribuyen por `org_id +
  product_surface`.

Readiness ejecutable antes de conectar un producto real:

```bash
bash scripts/onboarding/check-axis-readiness.sh
```

El check valida dos productos fake (`reference` y `shadow`) para demostrar que
Axis no depende de hardcode vertical ni de defaults Ponti/Pymes.

Cada producto debe tener un pack `scripts/evals/<product_surface>-golden.json`
con:

- `suite_id`;
- `product_surface`;
- `tenants.primary` y, si aplica, `tenants.shadow`;
- thresholds por metrica (`*_min` y `*_max`);
- casos de routing, tool selection, evidence, hallucination, tenant leakage y
  action safety.

Los packs son no bloqueantes al inicio (`non_blocking: true`), pero quedan
listos para volverse bloqueantes por threshold cuando el producto pase a
produccion.

El paquete `internal/productevals` carga y evalua packs genericos. El paquete
`internal/productcontracts` valida el onboarding reusable de un producto:

- producto registrado y activo;
- instalacion activa;
- identity/JWT context;
- manifests con conformance;
- writes con metadata Nexus;
- expected errors;
- eval pack;
- runtime enablement.

Checklist ejecutable:

```bash
go run ./cmd/product-onboarding-check \
  -contract /path/to/product-contract.json \
  -eval-pack scripts/evals/ponti-golden.json
```

El comando devuelve un reporte JSON y exit code `1` si hay fallas bloqueantes.

## Operacion

Axis expone una console operativa agregada para operar N productos sin UI
dedicada inicial:

- `GET /v1/ops/console`;
- `GET /v1/ops/alerts`;
- `GET /v1/ops/slos`.

Las rutas filtran por `org_id + product_surface` y derivan alertas de
instalaciones, conformance, eval reports, eventos, costos y runtime usage. La
capa es de lectura; no reemplaza los endpoints fuente de products,
installations, capabilities, observability, costos ni runtime policy.

## MCP Governance

MCP queda definido como capa operativa opcional sobre APIs y usecases Axis; no
duplica logica de negocio ni reemplaza los contratos HTTP principales.

Antes de implementar un servidor MCP productivo, Axis define un registry interno
de tools gobernadas. Cada tool MCP debe declarar:

- `name`, siempre bajo namespace `axis.*`;
- `required_scopes`, incluyendo `companion:mcp:execute`;
- `risk_level`;
- `side_effect_type`;
- `nexus_action_type`;
- `approval_required`.

Toda invocacion MCP futura debe pasar primero por `MCP Governance Gateway`, que
envia un request a Nexus con:

- `action_type=agent.capability.invoke`;
- `target_system=axis.mcp`;
- `target_resource=<tool_name>`;
- `org_id`;
- `product_surface`;
- `actor_id`;
- `required_scopes`;
- payload redaccionado;
- metadata de riesgo, side-effect y approval.

El servidor MCP, cuando exista, solo podra ejecutar una tool si el gateway
devuelve `can_execute=true`. Si Nexus devuelve `pending_approval`, el servidor
debe responder estado pendiente y no ejecutar. Si una tool marcada
`approval_required=true` recibe `allowed` sin aprobacion explicita, Axis falla
cerrado para obligar una policy Nexus correcta.

Superficies implementadas:

- `POST /mcp`: endpoint JSON-RPC para `initialize`, `ping`, `tools/list` y
  `tools/call`;
- `GET /v1/mcp/tools`: lista operativa de tools registradas;
- `POST /v1/mcp/tools/call`: invocacion operativa/debug de una tool.

Todas requieren autenticacion y scope `companion:mcp:execute`. `tools/call`
ademas exige los scopes propios de la tool y siempre consulta Nexus antes de
ejecutar. Las tools de lectura ejecutan usecases Axis existentes cuando Nexus
devuelve `can_execute=true`. Las tools sensibles solo ejecutan si Nexus devuelve
estado aprobado/ejecutado; si quedan pendientes, Axis responde
`pending_approval` sin tocar el sistema destino.

Tools iniciales gobernadas:

- `axis.products.list`
- `axis.products.get`
- `axis.installations.resolve`
- `axis.capabilities.validate`
- `axis.capabilities.import`
- `axis.traces.replay`
- `axis.costs.summary`
- `axis.evals.run`
- `axis.tasks.create`
- `axis.nexus.requests.list`
- `axis.ops.console`
- `axis.ops.alerts`
- `axis.ops.slos`

Regla de arquitectura: MCP es interfaz, Axis es executor y Nexus es regulador.

## Ponti

Ponti no se implementa en esta fase. Cuando avance, Ponti debe:

- registrar/publicar sus capabilities;
- mapear clientes Ponti a `org_id`;
- conectar chat a Companion con feature flag;
- empezar read-only;
- delegar writes sensibles a Nexus.

## Anexo (aditivo, post-freeze): Generic Product Connector — envelope `capability_execution.v1`

Este anexo es aditivo al contrato congelado; no modifica ninguna seccion
anterior. Define como un producto se conecta a Companion sin codigo vertical
en Axis: el `ProductConnector` generico descubre el manifest del producto y
ejecuta cualquier capability posteando un envelope unico. Un producto puede
publicar nuevas capabilities sin cambios de codigo en Axis.

### Habilitacion

- Flag global: `COMPANION_PRODUCT_CONNECTOR_GENERIC=true` (default local on).
- Por instalacion (`config` de la product installation):

```json
{
  "connector_mode": "envelope.v1",
  "discovery_path": "/api/v1/capabilities",
  "execute_path": "/api/v1/capability-executions"
}
```

- `connector_mode=envelope.v1` es obligatorio; `discovery_path` y
  `execute_path` son opcionales (defaults mostrados).
- Companion registra un connector generico por cada producto activo con al
  menos una instalacion habilitada en modo envelope. Los connectors legacy solo
  deben quedar como rollback explicito; Ponti legacy requiere
  `COMPANION_LEGACY_PONTI_CONNECTOR_ENABLED=true` y gana si comparte
  `product_surface`.
- La auth hacia el producto se resuelve POR CALL desde la instalacion de
  `org_id + product_surface`: `base_url` + bearer token resuelto del
  `secret_ref` (esquema `env:` en dev). No hay credencial global por env.
  Sin instalacion activa, Companion falla cerrado.

### Discovery

`GET {base_url}{discovery_path}` debe responder
`{ "items": [<capability_manifest.v1>, ...] }`. Companion fusiona los
manifests cuyo `product` coincide con el `product_surface` y normaliza cada
tool a una capability del registry (mismo puente que el connector Ponti:
mode/side-effect/risk/governance/idempotency). Cache con TTL de 5 minutos;
`POST /v1/connectors/refresh` fuerza re-discovery.

### Ejecucion

`POST {base_url}{execute_path}` con headers `Authorization: Bearer <secret>`,
`X-Tenant-Id: <external_tenant_id|org_id>` y, si aplica,
`X-Nexus-Request-ID: <uuid>` (mismo esquema que el connector Ponti). Body:

```json
{
  "schema_version": "capability_execution.v1",
  "operation": "agro.workorder.draft",
  "executor_ref": "agro-backend.workorder.draft",
  "payload": { "work_type": "spray", "workspace": { "project_id": 10 } },
  "workspace": { "project_id": 10 },
  "idempotency_key": "idem-1",
  "task_id": "<uuid>",
  "run_id": "run-7",
  "nexus_request_id": "<uuid>",
  "actor": {
    "actor_id": "user-456",
    "actor_type": "human",
    "on_behalf_of": "user-456",
    "product_surface": "agro"
  },
  "org_id": "org-123"
}
```

- `payload` es el input JSON de la capability, verbatim.
- `workspace` se extrae de `payload.workspace` cuando existe; sigue siendo
  JSON opaco para Axis.
- `executor_ref` proviene del manifest descubierto.
- `nexus_request_id` solo se envia en writes ya aprobados por Nexus.

Respuesta esperada:

```json
{
  "status": "success | partial | failure",
  "external_ref": "agro:exec:42",
  "result": {},
  "evidence": {},
  "error": "mensaje cuando status != success"
}
```

- `status` vacio se interpreta como `success`; cualquier otro valor no
  reconocido mapea a `failure`.
- `result` llega verbatim al `result_json` de la ejecucion.
- `evidence` del producto se mergea con el evidence canonico de identidad de
  Axis (`org_id`, `customer_org_id`, `actor_id`, `actor_type`,
  `companion_principal`, `on_behalf_of`, `service_principal`,
  `product_surface`, `capability_operation`, `workspace`, `source_ref`,
  `captured_at`). Las claves canonicas SIEMPRE ganan: el producto no puede
  pisar la atribucion. La sanitizacion de claves sensibles existente
  (payload/result) sigue aplicando en la capa de connectors.

### Auth de producto hacia Axis (JWT per-producto)

Complementario al envelope, los servicios Axis aceptan JWTs HS256 emitidos
por productos via `COMPANION_PRODUCT_JWT_KEYS` / `NEXUS_PRODUCT_JWT_KEYS`
(formato `product=<secret>|issuer=<issuer>[;product2=...]`). Claims esperados:
`iss` (issuer del producto), `aud` (servicio Axis receptor), `sub`/`actor_id`,
`org_id`, `product_surface`, `scopes`, `service_principal`, `on_behalf_of` y
`exp` corto. Estos principals quedan con `AuthMethod=product_jwt`: NO pueden
delegar decided_by en approvals de Nexus (ese gate sigue exigiendo
`api_key`). Detalle en `companion/docs/security.md`.
