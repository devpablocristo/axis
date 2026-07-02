# Operations

Companion se opera desde la raíz de Axis. El servicio no tiene Docker, Compose,
Makefile ni env example propios.

## Local

```bash
# desde axis/
test -f .env || cp .env.example .env
make up
make logs
```

Para hot reload en host:

```bash
# desde axis/
docker compose up -d companion-postgres nexus-postgres nexus
make dev-companion
```

## Health

- `GET /healthz`: proceso vivo.
- `GET /readyz`: DB disponible.

## Migrations

El backend aplica migraciones embebidas al arrancar. Validar versiones con:

```bash
bash scripts/quality/check-migrations.sh
```

## Background Loops

- `COMPANION_NEXUS_SYNC_INTERVAL_SEC`: sync de tasks con Nexus.
- `COMPANION_STRICT_NEXUS`: activa fail-closed estricto para grants Nexus.
- `COMPANION_WATCHER_INTERVAL_SEC`: encola ejecución periódica de watchers.
- `COMPANION_WATCHER_SYNC_INTERVAL_SEC`: encola reconciliación de watcher proposals.
- `COMPANION_JOB_WORKERS`: cantidad de workers durables. Default: `2`; `0`
  desactiva workers.
- `COMPANION_JOB_POLL_INTERVAL_SEC`: intervalo de polling de la queue durable.
- `COMPANION_JOB_LEASE_SEC`: duración del lease por claim.
- `COMPANION_JOB_TIMEOUT_SEC`: timeout default por job.
- `COMPANION_EMBEDDING_PROVIDER`: provider de embeddings (`vertex`,
  `vertex_ai` o `hash-v1` para dev/test).
- `COMPANION_EMBEDDING_MODEL`: modelo de embeddings persistido en memoria.
- `COMPANION_EMBEDDING_VERTEX_PROJECT`: proyecto GCP para embeddings Vertex.
- `COMPANION_EMBEDDING_VERTEX_LOCATION`: región Vertex. Default:
  `us-central1`.
- `COMPANION_EMBEDDING_DIMENSIONS`: dimensión esperada por provider/vector
  store.
- `COMPANION_OPS_ALERT_WEBHOOK_URL`: webhook opcional para despachar alertas
  operativas derivadas desde `POST /v1/ops/alerts/dispatch`.
- Memory purge corre cada hora.

## Jobs

- `GET /v1/jobs?status=dead_letter&product_surface=...` lista DLQ por
  producto.
- `GET /v1/jobs?status=running&product_surface=...` permite revisar leases,
  heartbeats, deadlines y jobs trabados.
- `GET /v1/jobs/{id}` devuelve estado, attempts, lease, evidence y errores.
- `POST /v1/jobs/{id}/cancel` cancela un job queued/running.
- `POST /v1/jobs/recover-expired` reencola leases vencidos.

Los endpoints requieren `companion:runtime:admin` o `companion:cross_org`.
Watchers usan jobs cuando la queue está configurada; si no, conservan el camino
inline para compatibilidad de desarrollo.

## Observability

- `GET /v1/run-traces/{run_id}/replay` devuelve el trace y eventos redacted de
  esa ejecución.
- `GET /v1/observability/events?run_id=...` lista eventos por run.
- `GET /v1/observability/events?limit=100` lista eventos recientes de la
  customer org autenticada.
- `GET /v1/observability/events?org_id=...&product_surface=...&event_type=...`
  lista eventos recientes usando filtros operativos combinados con `AND`.
- `GET /v1/tasks/{id}/graph` devuelve el ledger de execution graph para
  reconstruir planning/steps/checkpoints de una task.

Los eventos guardan `org_id`, `run_id`, `task_id`, `job_id`, `agent_id`,
`capability_id`, tipo/nombre de evento, payload redacted, severity y timestamp.
No se persisten secretos conocidos en payloads.

Filtros soportados por `GET /v1/observability/events`:

- `org_id`, `product_surface`, `run_id`;
- `event_type`, `event_name`, `severity`;
- `capability_id` y `tool_name` como alias operativo de `capability_id`;
- `agent_id`, `task_id`, `job_id`;
- `limit` entre 1 y 500.

Ejemplos de diagnostico:

```bash
# bloqueos MCP por runtime policy para una tool concreta
curl "$COMPANION_BASE/v1/observability/events?org_id=$ORG_ID&product_surface=companion&event_type=guardrail&event_name=mcp_runtime_policy&tool_name=axis.products.list&limit=50" \
  -H "X-API-Key: $COMPANION_API_KEY"

# auditoria MCP filtrada por tipo/nombre de evento
curl "$COMPANION_BASE/v1/observability/events?org_id=$ORG_ID&product_surface=companion&event_type=mcp&event_name=mcp_tool_call&limit=50" \
  -H "X-API-Key: $COMPANION_API_KEY"
```

Para correlacionar una llamada MCP gobernada: tomar el `request_id` devuelto
por la tool, consultar `GET /v1/requests/{request_id}` en Nexus, buscar el
evento `mcp/mcp_tool_call` en observability y, si hubo bloqueo operativo,
confirmar la alerta derivada en `GET /v1/ops/alerts`.

## modulo tecnico de agents

Para operacion de producto nueva, usar Virployees como superficie
publica:

- `GET /v1/virployees`: lista Virployees de la customer org.
- `POST /v1/virployees`: crea un Virployee.
- `PATCH /v1/virployees/{virployee_id}`: actualiza el core de un
  Virployee.
- `POST /v1/virployees/{virployee_id}/status`: cambia su estado.

Los endpoints de agents quedan como compatibilidad tecnica:

- `GET /v1/agents`: lista agentes tecnicos de la customer org.
- `PUT /v1/agents/{agent_id}`: crea o actualiza limites de un agente tecnico.
- `POST /v1/agents/{agent_id}/disable`: kill switch por agente.
- `POST /v1/agents/handoffs`: registra handoff entre agentes.

Los endpoints requieren `companion:runtime:admin` o `companion:cross_org`. El
chat puede enviar `agent_id`; si el agente no está activo, Companion falla
cerrado antes de invocar el LLM.

## Security evals

- `GET /v1/security-evals/suites` lista suites disponibles.
- `POST /v1/security-evals/runs` ejecuta una suite y guarda el reporte.
- `GET /v1/security-evals/reports` lista reportes persistidos.

Los endpoints requieren scopes admin de runtime/evals o cross-org. El check
local obligatorio sigue siendo `bash scripts/evals/run-security-evals.sh`.

## Product evals y onboarding

- `scripts/evals/<product>-golden.json` define golden cases por producto.
- `bash scripts/evals/run-product-evals.sh` valida packs y tests genericos.
- `go run ./cmd/product-onboarding-check -contract <contract.json> -eval-pack
  scripts/evals/<product>-golden.json` ejecuta el checklist de onboarding.
- `bash scripts/onboarding/check-reference-product.sh` valida el producto
  generico `reference` sin conectar ningun producto real.
- `bash scripts/onboarding/check-axis-readiness.sh` valida los productos fake
  `reference` y `shadow`, comprueba que usen `product_surface` y `org_id`
  distintos, y falla si algun fixture usa defaults reales como Ponti/Pymes.
- `AXIS_REAL_PRODUCTS=ponti,medmory bash scripts/onboarding/check-axis-readiness.sh`
  agrega contratos reales al mismo gate sin convertir Ponti/Medmory en defaults
  hardcodeados de Axis.
- `scripts/onboarding/seed-product-installation.sh` registra cualquier producto
  externo usando env vars como `PRODUCT_SURFACE`, `PRODUCT_ORG_ID`,
  `PRODUCT_BASE_URL` y `PRODUCT_SECRET_REF`.

Los evals de producto son no bloqueantes al inicio; el reporte mantiene
thresholds por producto para convertirlos en gate cuando el producto tenga
suficiente cobertura.

## Ops console

- `GET /v1/ops/console` devuelve una vista agregada por `org_id` y
  `product_surface`: products, installations, capabilities, conformance,
  security eval reports, cost summary, runtime policy/usage, runtime limits,
  metricas, salud de jobs, eventos, alertas y SLOs.
- `GET /v1/ops/alerts` devuelve solo alertas derivadas.
- `GET /v1/ops/metrics` devuelve metricas derivadas por producto: eventos,
  tool/MCP calls, errores, guardrails, costo, eval score y latency.
- `GET /v1/ops/slos` devuelve SLOs por producto.
- `POST /v1/ops/alerts/dispatch` envia las alertas derivadas al webhook
  configurado. Requiere `companion:runtime:admin` o `companion:cross_org`.

`runtime_limits` es una vista derivada, no una segunda fuente de verdad. Cruza
`cost_summary` con `runtime_policy` para mostrar uso vs limite por
`product_surface` cuando hay budget de costo o tools configurado. Si el limite
viene de politica global, `*_source` indica `org_control_plane` o
`org_runtime_policy`; si viene de politica puntual de producto indica
`product_policy`.

Las alertas iniciales son reglas deterministicas y baratas de operar:
installation/product disabled, conformance failed, eval regression,
tenant/product leakage signals, runtime budget blocks, cost near/exhausted, high
tool error rate, MCP runtime policy blocks, jobs en DLQ, jobs trabados, leases
vencidos y rate limit abuse. Eventos repetidos del mismo tipo/producto/target
se agrupan en una ventana corta y exponen `suppressed_count` en la evidencia
para evitar ruido operativo. Los SLOs iniciales cubren latency, availability,
tool success rate, eval score y cost ceiling. Latency se deriva de
`duration_ms` redacted en observability events; si no hay muestras recientes,
queda `unknown`.

Ejemplos:

```bash
# metricas por producto
curl "$COMPANION_BASE/v1/ops/metrics?org_id=$ORG_ID&product_surface=companion&limit=100" \
  -H "X-API-Key: $COMPANION_API_KEY"

# SLOs por producto
curl "$COMPANION_BASE/v1/ops/slos?org_id=$ORG_ID&product_surface=companion" \
  -H "X-API-Key: $COMPANION_API_KEY"

# despachar alertas al webhook configurado
curl -X POST "$COMPANION_BASE/v1/ops/alerts/dispatch?org_id=$ORG_ID&product_surface=companion" \
  -H "X-API-Key: $COMPANION_API_KEY"
```

El webhook recibe un JSON redacted con `org_id`, `product_surface`, `period`,
`generated_at` y `alerts`. Si `COMPANION_OPS_ALERT_WEBHOOK_URL` no está
configurado, el endpoint devuelve `503 NOT_CONFIGURED`.

## Retention

Estado actual:

- Memory expirada se purga con `memory.retention` / `PurgeExpired`.
- Runtime policy ya contiene settings de retention (`run_trace_days`,
  `tool_evidence_days`, `memory_days`) para gobernanza.
- Observability events, traces, eval reports y cost ledger todavía no tienen
  borrado automático dedicado en este corte.

Runbook hasta automatizar cleanup:

1. Definir retention por org/producto desde runtime policy.
2. Exportar evidencia necesaria antes de borrar (`run replay`, cost summary,
   eval reports).
3. Ejecutar cleanup manual por ventana temporal solo con aprobación operativa.
4. Registrar el cambio en el log/release notes del servicio.

No automatizar deletes de traces/cost/evals antes de tener SLOs de auditoria y
retención por tipo. Ese job debería agregarse como fase separada con tests de
no-leakage y dry-run.

## MCP runbook

MCP es una capa operativa sobre APIs Axis existentes. No debe duplicar lógica de
negocio ni saltar Nexus. El orden de control para `tools/call` es:

1. auth principal + scope global `companion:mcp:execute`;
2. scopes granulares declarados por tool;
3. runtime policy MCP por org/product/tool;
4. rate limit por `org_id + product_surface`;
5. Nexus allow/deny/pending;
6. ejecución real de la API Axis.

`tools/list` filtra por scopes efectivos y por runtime policy cuando hay
contexto de org/product disponible. Una tool sin scope granular no aparece en la
lista y tampoco ejecuta si se llama manualmente.

Bloquear por scope:

- Revisar `annotations.required_scopes` en `tools/list`.
- Si falta scope, `tools/call` responde `status=blocked` con
  `metadata.blocked_by=scope`.
- Observability registra `event_type=guardrail`,
  `event_name=mcp_scope_required`.

Bloquear o permitir por runtime policy:

```bash
# bloquear una tool MCP puntual
curl -X PUT "$COMPANION_BASE/v1/runtime/mcp-policy" \
  -H "X-API-Key: $COMPANION_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"denied_tools":["axis.products.list"]}'

# permitir solo tools de products
curl -X PUT "$COMPANION_BASE/v1/runtime/mcp-policy" \
  -H "X-API-Key: $COMPANION_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"allowed_tools":["axis.products.*"],"denied_tools":[]}'
```

Auditar:

```bash
# scope faltante
curl "$COMPANION_BASE/v1/observability/events?org_id=$ORG_ID&product_surface=companion&event_type=guardrail&event_name=mcp_scope_required&limit=50" \
  -H "X-API-Key: $COMPANION_API_KEY"

# runtime policy
curl "$COMPANION_BASE/v1/observability/events?org_id=$ORG_ID&product_surface=companion&event_type=guardrail&event_name=mcp_runtime_policy&limit=50" \
  -H "X-API-Key: $COMPANION_API_KEY"

# llamadas MCP
curl "$COMPANION_BASE/v1/observability/events?org_id=$ORG_ID&product_surface=companion&event_type=mcp&event_name=mcp_tool_call&limit=50" \
  -H "X-API-Key: $COMPANION_API_KEY"
```

Para diagnostico completo: tomar `request_id` si existe, revisar Nexus, luego
buscar `mcp/mcp_tool_call` y posibles guardrails asociados. Si no hay
`request_id`, el bloqueo ocurrió antes de Nexus.

## Smoke

```bash
# desde axis/
make smoke-companion
```

Los smoke scripts esperan Companion y Nexus levantados, y usan las keys de
`.env` en la raíz de Axis.

El smoke MCP (`companion/scripts/smoke/run-companion-mcp-flow.sh`) verifica:

- `POST /mcp` con `initialize`;
- `tools/list`;
- `tools/call axis.products.list` con policy Nexus `allow`;
- `tools/call axis.evals.run` con policy Nexus `require_approval`;
- `tools/call axis.tasks.create` con policy Nexus `deny`;
- que una tool denegada no ejecute la creación de task;
- que las llamadas MCP se auditen usando filtros server-side de observability.

El smoke de runtime policy MCP (`companion/scripts/smoke/run-companion-mcp-runtime-policy-flow.sh`) verifica:

- `PUT /v1/runtime/mcp-policy` para bloquear `axis.products.list`;
- que `tools/call axis.products.list` falle con JSON-RPC `status=403` y `mcp_status=blocked`;
- que el bloqueo ocurra antes de Nexus, sin `request_id`;
- que se registre `guardrail/mcp_runtime_policy` usando filtros server-side de
  observability;
- que `GET /v1/ops/alerts` exponga `mcp_runtime_policy_block`;
- que `allowed_tools=["axis.products.*"]` vuelva a permitir la tool y pase por Nexus.

Las policies Nexus locales para MCP pueden seedearse de forma idempotente con:

```bash
# desde axis/
bash nexus/scripts/seed-axis-mcp-policies.sh
```

Estas policies gobiernan `target_system=axis.mcp` y
`action_type=agent.capability.invoke`.

La runtime policy MCP corre antes de Nexus. Sirve como barrera operativa por
org/product/tool; Nexus sigue regulando aprobación/denegación de ejecución.
