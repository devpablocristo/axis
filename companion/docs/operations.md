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
- Memory purge corre cada hora.

## Jobs

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

## Agent Fleet

- `GET /v1/agents`: lista empleados IA de la customer org.
- `PUT /v1/agents/{agent_id}`: crea o actualiza límites de un empleado IA.
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

Los evals de producto son no bloqueantes al inicio; el reporte mantiene
thresholds por producto para convertirlos en gate cuando el producto tenga
suficiente cobertura.

## Ops console

- `GET /v1/ops/console` devuelve una vista agregada por `org_id` y
  `product_surface`: products, installations, capabilities, conformance,
  security eval reports, cost summary, runtime policy/usage, eventos, alertas y
  SLOs.
- `GET /v1/ops/alerts` devuelve solo alertas derivadas.
- `GET /v1/ops/slos` devuelve SLOs por producto.

Las alertas iniciales son reglas deterministicas y baratas de operar:
installation/product disabled, conformance failed, eval regression,
tenant/product leakage signals, cost near/exhausted, high tool error rate y
rate limit abuse. Los SLOs iniciales cubren availability, tool success rate,
eval score y cost ceiling; latency queda `unknown` hasta persistir latencias por
evento/tool.

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

Ejemplos:

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
