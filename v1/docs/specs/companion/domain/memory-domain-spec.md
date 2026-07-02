# Memory Domain Spec

## Proposito

Este spec separa `Memory` de `MemoryEntry`.

Regla:

```text
Memory = contenedor asignable a un employee
MemoryEntry = dato recordado dentro de una memoria
```

## Memory

Definicion: contenedor de memoria persistente asignable a un
`Virployee`.

Utilidad: permite activar, desactivar y gobernar memoria como entidad fuerte.

No representa: una nota individual, un vector, una conversacion ni un scope
tecnico.

Tipo: entidad fuerte.

CRUD objetivo: si.

Audiencia: operativa/admin.

Modelo objetivo:

```text
Memory
- memory_id: UUID
- tenant_id: UUID
- owner_virployee_id: UUID
- policy: MemoryPolicy
- status: MemoryStatus
```

Enums:

```text
MemoryStatus: active, disabled, archived
```

Relaciones:

```text
Memory.tenant_id -> Tenant.tenant_id
Memory.owner_virployee_id -> Virployee.virployee_id
Virployee.memory_id -> Memory.memory_id | null
MemoryEntry.memory_id -> Memory.memory_id
```

Estado actual: existe `companion_memory_entries` con `id uuid`,
`scope_type`, `scope_id`, `kind`, contenido y governance. Tambien existen
vectors, reviews, summaries y audit.

Brecha: no hay entidad contenedora `Memory` clara para que Employee apunte a
`memory_id`.

## MemoryEntry

Definicion: dato recordado dentro de una `Memory`.

Utilidad: almacena hechos, preferencias, eventos o contexto persistente.

No representa: la memoria completa del employee ni el policy de memoria.

Tipo: entidad fuerte secundaria dentro de `Memory`.

CRUD objetivo: si limitado/gobernado.

Audiencia: operativa/admin avanzada.

Modelo objetivo:

```text
MemoryEntry
- memory_entry_id: UUID
- memory_id: UUID
- kind: MemoryEntryKind
- content_text: string
- confidence: decimal
- status: MemoryEntryStatus
```

Enums:

```text
MemoryEntryKind: task_summary, semantic_fact, user_preference, operational_state, procedure
MemoryEntryStatus: active, superseded, conflict, rejected, forgotten
```

Relaciones:

```text
MemoryEntry.memory_id -> Memory.memory_id
```

Estado actual: `companion_memory_entries.id` funciona como ID de entrada. Hay
campos de scope, payload, embeddings y governance.

Brecha: `scope_type/scope_id` son mecanismo tecnico; el modelo de Employee
necesita `Memory.memory_id`.

## MemoryPolicy

Definicion: regla simple de retencion y scopes permitidos.

Tipo: value object.

Modelo objetivo:

```text
MemoryPolicy
- enabled_by_default: boolean
- retention_days: integer
- allow_user_memory: boolean
- allow_task_memory: boolean
- allow_tenant_memory: boolean
```

## Auditoria Actual Vs Objetivo

| Concepto actual | Problema | Modelo objetivo |
|---|---|---|
| `memory_scope_id` | Scope tecnico, no entidad. | `memory_id UUID`. |
| `companion_memory_entries.id` | ID de entrada, no contenedor del employee. | `Memory.memory_id` separado de `MemoryEntry.memory_entry_id`. |
| `payload_json` | Dato flexible sin forma en el core. | `content_text` y value objects especificos fuera de este core. |
| `memory_enabled` | Booleano insuficiente. | `memory_id null` para sin memoria; `Memory.status` para estado. |

