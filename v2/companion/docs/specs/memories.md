# Virployee Memory v2

## Decision

Companion owns one persistent memory model scoped exclusively by `tenant_id` and
`virployee_id`. It replaces the v1 overlap between containers, facts and
operational memory. There is no v1 migration and no tenant-, user-, task- or
conversation-shared memory.

Memories have a title, lexical content, type (`fact`, `preference`, `procedure`
or `note`), sensitivity (`normal` or `sensitive`), provenance, actor, content
hash, optimistic version, lifecycle state, trust score and review state. Every
human, system and accepted-learning write passes through `MemoryCuratorPort`;
callers cannot select their own trust or bypass secret/PII, prompt-poisoning and
conflict checks. Public writes always use `human` provenance; `system` is
available only through Companion's internal port.

## Security and lifecycle

Only the assigned supervisor or tenant `owner`/`admin` may read, recall or
mutate a Virployee's memories. BFF discards caller-supplied role headers and
forwards the resolved membership role; Companion independently checks tenant,
actor, role and supervisor. List responses redact sensitive content. Authorized
detail is the only public response containing full sensitive content.

States are `active`, `archived` and `trash`; review states are `approved`,
`pending`, `quarantined` and `rejected`. Secret-bearing input is rejected before
persistence. Prompt-poisoning and conflicting facts/procedures are quarantined
for explicit human review. The LLM cannot set trust, approve a memory or move a
quarantined record into recall. Recall includes only active, approved,
non-expired records above the trust floor. A durable PostgreSQL job applies
bounded trust decay and cleanup; process-local tickers only wake the worker.
Create, update, review and lifecycle actions append an audit record containing
hashes, versions and curation metadata, never content.

## Retrieval and runtime

F5 recall is hybrid pgvector plus PostgreSQL full-text search, always constrained
by tenant and Virployee before ranking. Embeddings use Runtime's
`gemini-embedding-001` adapter at 768 dimensions and carry model plus content
version so stale vectors never match current text. Failed or pending indexing
falls back to lexical recall without weakening curation predicates. Recall
returns five results by default and ten at most. Lists use an opaque cursor over
`updated_at + id`, default 50 and maximum 100.

Every memory indexing or vector-query call reserves the tenant's `axis / embeddings`
quota before reaching Runtime. A denied indexing job remains retryable; denied
or unavailable query embedding degrades to the same tenant-scoped lexical
recall. Successful calls append only token estimates, model and operational
identifiers to the usage ledger—never query text, memory content or vectors.

Runtime Context and Dry Run expose safe memory references (ID, title, type,
version, content hash, sensitivity and score). Run traces persist those
references plus a deterministic `memory_context_hash`, never memory content.
Execution Gate binds that hash so governance seals the exact recalled context.
The recalled, curator-approved content is included in the Runtime proposal
context while run traces retain only references and the hash. The deterministic
fallback parser does not change its decision from memory.

Runtime responses include an estimated input/output token and cost envelope
until provider-authoritative usage is available. Companion reserves the LLM
budget before a run enters `answering`, records actual returned estimates after
the call, and keeps all prompt and document content out of accounting metadata.

## Explicit non-goals

Documentary evidence from Medmory is never promoted into memory automatically;
it remains in `artifactindex`. Curation detects deterministic conflicts but does
not ask an LLM to resolve them. PostgreSQL encryption at rest protects content;
field encryption is deferred because it would prevent lexical and vector
retrieval.
