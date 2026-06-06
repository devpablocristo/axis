# Testing

## Suites obligatorias

- Unit: FSM de tasks, decision mapping Nexus, authz, memory validation, agent
  routing y tool policy.
- Integration: repositories Postgres, migrations, tasks, memory, connectors y
  run traces.
- Contract: Nexus mock para `allow`, `deny`, `require_approval`, `approved`,
  `rejected`, `executed`, evidence y result reporting.
- Multi-tenant: acceso cruzado denegado para tasks, memory, connectors,
  watchers y traces.
- Security: prompt injection, scopes, body limits, secret masking.
- Regression: smoke scripts Companion + Nexus.
- Product evals: packs `scripts/evals/<product>-golden.json` con routing,
  tool selection, evidence, hallucination, tenant leakage y action safety.
- Product contracts: onboarding spec local validado con
  `cmd/product-onboarding-check`.

## Comandos

```bash
go test ./... -count=1
go vet ./...
bash scripts/quality/check-migrations.sh
bash scripts/quality/check-nexus-imports.sh
bash scripts/quality/check-side-effects-pipeline.sh
bash scripts/evals/run-product-evals.sh
```

## Fixtures

Los tests no deben requerir LLM real. Para Nexus usar fakes/mocks y cubrir el
contrato de estados. Para productos, preferir manifest/capability fakes antes
que servicios reales.
Los eval packs de producto deben ser reproducibles localmente; al inicio son no
bloqueantes para deploy, pero deben exponer thresholds para volverlos
bloqueantes por producto cuando haya datos suficientes.
