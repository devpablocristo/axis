# Companion

Servicio headless de IA transversal de Axis. Companion concentra runtime LLM,
agentes, memoria, tools, planificación y ejecución asistida para ser consumido
por productos, gateways y servicios internos. Consume Nexus para
toda acción sensible que requiera policy, approval, risk o audit fuerte.

> La DB se llama `companion`; el módulo Go es `github.com/devpablocristo/companion`.

## Boundaries

- **IA = Companion**, **Decisiones sensibles = Nexus**, sin excepciones.
- Companion no evalúa policies, no aprueba/rechaza requests y no reimplementa
  el risk engine.
- Companion no posee UI propia; la administración vive en `../console` vía
  `../bff`.
- Docker, compose y Make targets viven en la raíz de Axis.

## Estructura

```text
companion/
├── cmd/api/
├── internal/
├── wire/
├── migrations/
├── scripts/
├── docs/
├── go.mod
├── go.sum
└── openapi.yaml
```

## Desarrollo

Desde la raíz de Axis:

```bash
make test-companion
make qa-companion
make dev-companion
make smoke-companion
docker compose up -d --build companion-postgres companion
```

URL por defecto: `http://localhost:18085`.

## Variables principales

- `COMPANION_API_KEYS`
- `COMPANION_AUTH_ISSUER_URL`
- `COMPANION_AUTH_AUDIENCE`
- `COMPANION_INTERNAL_JWT_*`
- `NEXUS_BASE_URL`
- `NEXUS_API_KEY`
- `COMPANION_LLM_*`

## Documentación

- `companion/architecture.md`
- `companion/boundaries.md`
- `companion/domain-model.md`
- `companion/memory.md`
- `companion/agents.md`
- `companion/virtual-employees.md`
- `companion/job-roles.md`
- `companion/workforce-platform-debt.md`
- `companion/workforce-implementation-gap.md`
- `companion/tools.md`
- `companion/nexus-integration.md`
- `companion/security.md`
- `companion/testing.md`
- `companion/operations.md`
- `companion/product-integration-contract-v1.md`
- `../companion/openapi.yaml`
