# Nexus Enterprise Governance Hardening

Este documento resume las capacidades agregadas para convertir Nexus en un
control plane de governance más verificable y operable, sin convertirlo en
runtime IA ni acoplarlo a Companion.

## Contratos de governance

Nexus mantiene contratos versionados en `governance_contracts`.

- `tool_intent` / `tool_intent.v1` valida `action_binding`.
- `result_report` / `result_report.v1` valida reportes de ejecución.
- `validation_mode=report_only` registra violaciones sin bloquear.
- `validation_mode=enforce` falla cerrado cuando el payload no cumple.

Endpoints:

- `GET /v1/governance/contracts`
- `POST /v1/governance/contracts`
- `POST /v1/governance/contracts/validate`

## Audit tamper-evident

Los nuevos `request_events` se sellan con:

- `payload_hash`
- `previous_hash`
- `event_hash`
- `signature_key_id`
- `signature`

El chain scope default es el `request_id`. Los eventos históricos previos a la
migración quedan como legacy/unsealed.

Endpoints:

- `GET /v1/requests/{id}/replay`
- `GET /v1/requests/{id}/replay/verify`

## Callback outbox

Los callbacks de approvals ahora entran en un outbox durable:

- `nexus_outbox_events`
- `nexus_callback_deliveries`

El worker entrega at-least-once, firma callbacks con
`X-Nexus-Callback-Signature` cuando `NEXUS_CALLBACK_TOKEN` está configurado,
reintenta con backoff y marca `dead` al agotar intentos. El claim de deliveries
usa lease transaccional para evitar doble procesamiento entre workers.

Endpoints operativos:

- `GET /v1/ops/callback-deliveries`
- `POST /v1/ops/callback-deliveries/{id}/retry`

## Policy lifecycle v2

Las policies existentes se backfillean a:

- `policy_artifacts`
- `policy_versions`
- `policy_changelog`
- `policy_promotions`

La API v1 sigue compatible. Cada create/update por `/v1/policies` registra una
versión inmutable asociada al policy legacy.

Endpoints:

- `GET /v1/policies/{id}/versions`
- `GET /v1/policies/{id}/changelog`
- `GET /v1/policies/{id}/promotions`
- `POST /v1/policies/{id}/promotions`
- `POST /v1/policy-promotions/{id}/approve`
- `POST /v1/policy-promotions/{id}/enforce`
- `POST /v1/policy-promotions/{id}/rollback`

Las promociones requieren `dry_run_report`, tienen separation-of-duties en la
aprobación y respetan freeze windows antes de aplicar una versión enforced.
El changelog registra requester, approver, enforcer y rechazos por SoD
(`promotion_approval_denied`) con `promotion_id` como correlation id.

Para desarrollo y E2E local existen dos identidades admin separadas en la misma
org:

- `NEXUS_ADMIN_A_API_KEY` → actor `nexus-admin-a`
- `NEXUS_ADMIN_B_API_KEY` → actor `nexus-admin-b`

También existe `NEXUS_OTHER_ORG_ADMIN_API_KEY` → actor
`nexus-admin-other` en `AXIS_OTHER_ORG_ID` para probar aislamiento tenant. SoD
se evalúa contra el actor resuelto desde `NEXUS_API_KEYS`; por eso una promotion
solicitada por Admin A debe devolver `409` si Admin A intenta aprobarla, y debe
ser aprobada por Admin B.

E2E específico:

```bash
make e2e-nexus-policy-promotion
```

`make e2e-nexus` también ejecuta este flujo además del lifecycle histórico.

## Simulación e impacto

`POST /v1/requests/simulate/replay` conserva su contrato previo, pero ahora
persiste un `policy_simulation_run` con `report_hash` y samples. Esto permite
usar el resultado como evidencia de promotion y comparar impacto histórico sin
depender de un cálculo efímero.

## Legal hold, retention y exports

Se agregan foundations operativas para governance retention:

- `governance_legal_holds`
- `governance_retention_policies`
- `governance_export_jobs`

Endpoints:

- `GET /v1/ops/legal-holds`
- `POST /v1/ops/legal-holds`
- `GET /v1/ops/exports`
- `POST /v1/ops/exports`

Los exports crean un manifest hasheado. La política default sigue siendo no
destructiva: no hay cleanup destructivo automático sin legal hold/export
verificado.

## Rate limiting

`nexus_rate_limit_rules` ahora tiene enforcement Postgres-backed mediante
`nexus_rate_limit_counters` y decisiones auditables en
`nexus_rate_limit_decisions`.

Las reglas pueden aplicarse por org, principal y endpoint. `mode=report_only`
registra sin bloquear; `mode=enforce` responde `429` cuando se supera el límite.

## Reconciliación

Endpoint:

- `POST /v1/ops/reconciliation/run`

La reconciliación detecta fallos operativos como:

- audit integrity checks fallidos
- callback deliveries muertas
- callback deliveries con lease vencido
- approvals pendientes ya expiradas

Los findings se persisten en `nexus_reconciliation_findings` con severidad y
hash de reporte.

## Observabilidad

Nexus puede inicializar OpenTelemetry tracing con:

- `NEXUS_TRACING_EXPORTER=none|stdout|otlp`
- `NEXUS_OTEL_EXPORTER_OTLP_ENDPOINT`
- `NEXUS_OTEL_EXPORTER_OTLP_INSECURE`
- `NEXUS_TRACING_SAMPLE_RATIO`

Por defecto queda no-op para desarrollo/tests. Los spans HTTP se emiten después
de autenticación y antes de rate limiting para conservar correlación.

## Approval plans

Cada approval nueva genera un snapshot en `approval_plans`. Las approvals
break-glass generan además `break_glass_reviews`.

El usecase impide que el requester apruebe su propia request cuando el
`requester_id` está disponible, preservando separación de duties básica.

## Operación

Endpoint de diagnóstico:

- `GET /v1/ops/governance/summary`

Incluye:

- contratos activos
- validaciones fallidas
- callbacks pendientes/dead
- callbacks stuck
- integridad audit fallida
- reglas rate-limit activas
- legal holds activos
- exports fallidos
- findings críticos de reconciliación

## Invariantes preservadas

- Nexus no importa runtime IA, memoria, agentes ni Companion internals.
- Las decisiones siguen siendo determinísticas y policy-driven.
- Los endpoints v1 existentes conservan compatibilidad.
- Los nuevos controles son aditivos y tenant-aware.
