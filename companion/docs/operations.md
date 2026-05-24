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

Los eventos guardan `org_id`, `run_id`, `task_id`, `job_id`, `agent_id`,
`capability_id`, tipo/nombre de evento, payload redacted, severity y timestamp.
No se persisten secretos conocidos en payloads.

## Agent Fleet

- `GET /v1/agents`: lista empleados IA de la customer org.
- `PUT /v1/agents/{agent_id}`: crea o actualiza límites de un empleado IA.
- `POST /v1/agents/{agent_id}/disable`: kill switch por agente.
- `POST /v1/agents/handoffs`: registra handoff entre agentes.

Los endpoints requieren `companion:runtime:admin` o `companion:cross_org`. El
chat puede enviar `agent_id`; si el agente no está activo, Companion falla
cerrado antes de invocar el LLM.

## Smoke

```bash
# desde axis/
make smoke-companion
```

Los smoke scripts esperan Companion y Nexus levantados, y usan las keys de
`.env` en la raíz de Axis.
