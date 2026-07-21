# Virployee Memory v2

## Decision

Companion owns one persistent memory model scoped exclusively by `tenant_id` and
`virployee_id`. It replaces the v1 overlap between containers, facts and
operational memory. There is no v1 migration and no tenant-, user-, task- or
conversation-shared memory.

Memories have a title, lexical content, type (`fact`, `preference`, `procedure`
or `note`), sensitivity (`normal` or `sensitive`), provenance, actor,
content hash, optimistic version and lifecycle state. Public writes always use
`human` provenance; `system` is available only through Companion's internal
port.

## Security and lifecycle

Only the assigned supervisor or tenant `owner`/`admin` may read, recall or
mutate a Virployee's memories. BFF discards caller-supplied role headers and
forwards the resolved membership role; Companion independently checks tenant,
actor, role and supervisor. List responses redact sensitive content. Authorized
detail is the only public response containing full sensitive content.

States are `active`, `archived` and `trash`. Recall includes active records
only. Purge deletes a trashed record immediately (trash is required first, but
there is no retention wait). Create, update and lifecycle actions append an
audit record containing hashes and versions, never content.

## Retrieval and runtime

Recall uses PostgreSQL full-text search with configuration `simple`, always
constrained by tenant and Virployee. It returns five results by default and ten
at most, ordered by lexical rank, update time and ID. Lists use an opaque cursor
over `updated_at + id`, default 50 and maximum 100.

Runtime Context and Dry Run expose safe memory references (ID, title, type,
version, content hash, sensitivity and score). Run traces persist those
references plus a deterministic `memory_context_hash`, never memory content.
Execution Gate binds that hash so governance seals the exact recalled context.
The deterministic parser does not change its decision from memory yet.

## Explicit non-goals

This cut has no embeddings, pgvector, model provider, chat, automatic
extraction/writes, semantic conflict resolution, decay or automatic curation.
PostgreSQL encryption at rest protects content; field encryption is deferred
because it would prevent lexical search. Platform is unchanged because its
Python runtime cannot consume Companion's Go implementation directly and there
is not yet a second consumer.
