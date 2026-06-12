# Companion Enterprise Platform

Esta entrega cierra superficies productivas para operar empleados IA por
customer org sin meter conocimiento vertical en Companion.

## Boundaries

- Companion mantiene runtime IA, memoria, agents, capabilities, jobs y replay.
- Nexus sigue siendo el boundary de decisión para acciones sensibles. Companion
  arma intent, binding, evidence y reportes; Nexus decide allow, deny o
  approval.
- BFF es el único camino productivo desde Console hacia Companion/Nexus.

## Nuevas superficies

- Capability fabric: `/v1/capabilities`, validación, import, promoción,
  deprecación y conformance runs.
- Memory v2 operativa: vectores por namespace `org_id:product_surface[:agent_id]`,
  reviews, conflicts, summaries, audit, export y delete por org.
- Embeddings productivos: adapter `EmbeddingProvider`, `VectorStore`
  `pgvector` cuando esté disponible y fallback JSON auditado.
- Durable jobs: handlers para embedding, retention, decay y compaction, además
  de listado operacional en `/v1/jobs`.
- Observabilidad y costo: eventos redacted, replay, graph replay por task y
  `/v1/runtime/costs`.
- Agent fleet: assignment automático ponderado, ownership de task y handoffs
  ejecutables bajo el mismo scope org/product.
- Control plane: settings de embeddings, model routing, eval thresholds,
  observability, budgets y data residency.
- Security evals: suites ejecutables por API y reportes persistidos.

## Invariantes

- Sin `org_id` efectivo no se ejecutan operaciones tenant-aware.
- Sin manifest válido no se publica capability.
- Side effects siguen requiriendo binding compatible con Nexus.
- Memoria durable requiere scope, provenance, confidence y namespace.
- Los endpoints admin requieren scopes internos emitidos por BFF.
- Sin presupuesto disponible, manifest activo, agente válido y evidence
  requerida, Companion falla cerrado antes de ejecutar side effects.

## Operación local

Validaciones recomendadas:

```bash
make check-companion
make qa-companion
make test-bff
cd console && npm run typecheck && npm run build
```
