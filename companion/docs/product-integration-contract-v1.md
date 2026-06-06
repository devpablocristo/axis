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
- `config` no puede contener secretos planos; se usan referencias seguras.

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

## Ponti

Ponti no se implementa en esta fase. Cuando avance, Ponti debe:

- registrar/publicar sus capabilities;
- mapear clientes Ponti a `org_id`;
- conectar chat a Companion con feature flag;
- empezar read-only;
- delegar writes sensibles a Nexus.
