# Virployee Memory v2

## Decision

Companion owns one persistent memory model under `org_id + virployee_id`,
with an explicit nested work scope. It replaces the v1 overlap between
containers, facts and operational memory. There is no v1 migration and no
organization-, user- or conversation-shared memory outside these boundaries.
Its physical tables use the `companion_virployee_*` namespace so a shared
database can retain v1's unrelated product-memory tables without collision or
implicit migration.

The valid scopes are:

- `virployee`: non-personal procedures and durable operating knowledge shared
  across the Virployee's work.
- `subject`: preferences and history for exactly one patient/customer/work
  subject.
- `case`: temporary or case-specific facts for exactly one subject and Assist
  case.

`subject` requires `subject_id` and forbids `case_id`; `case` requires both;
`virployee` accepts neither. Existing memory rows are retained as
Virployee-global records. A case-scoped write must reference a case for the same
organization, subject and responsible/entrypoint Virployee.

Memories have a title, lexical content, type (`fact`, `preference`, `procedure`
or `note`), sensitivity (`normal` or `sensitive`), provenance, actor, content
hash, optimistic version, lifecycle state, trust score and review state. Every
human, system and accepted-learning write passes through `MemoryCuratorPort`;
callers cannot select their own trust or bypass secret/PII, prompt-poisoning and
conflict checks. Public writes always use `human` provenance; `system` is
available only through Companion's internal port.

## Security and lifecycle

Only the assigned supervisor or organization `owner`/`admin` may read, recall or
mutate a Virployee's memories. BFF discards caller-supplied role headers and
forwards the resolved membership role; Companion independently checks organization,
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

F5 recall is hybrid pgvector plus PostgreSQL full-text search, always
constrained by organization, Virployee and requested work scope before ranking.
Embeddings use Runtime's `gemini-embedding-001` adapter at 768 dimensions and
carry model plus content version so stale vectors never match current text.
Failed or pending indexing falls back to lexical recall without weakening
curation predicates. Recall returns five results by default and ten at most.
Lists use an opaque cursor over `updated_at + id`, default 50 and maximum 100.

Listing selects one exact scope. Recall is deliberately hierarchical:

```text
virployee request -> virployee-global only
subject request   -> virployee-global + exact subject
case request      -> virployee-global + exact subject + exact case
```

No query scans a sibling subject or case. Conflict detection, active-content
uniqueness, curation audit and safe Runtime references also carry the scope, so
two patients assigned to one Virployee cannot collide or leak through recall.

Every memory indexing or vector-query call reserves the trusted request
product's `embeddings` quota before reaching Runtime. Internal jobs use the
explicit `platform-internal` attribution rather than pretending to belong to a
consumer. A denied indexing job remains retryable; denied
or unavailable query embedding degrades to the same organization-scoped lexical
recall. Successful calls append only token estimates, model and operational
identifiers to the usage ledger—never query text, memory content or vectors.

Runtime Context and Dry Run expose safe memory references (ID, title, type,
version, content hash, sensitivity and score). Run traces persist those
references plus a deterministic `memory_context_hash`, never memory content.
Execution Gate binds that hash so governance seals the exact recalled context.
For `grounding_mode=general`, the recalled, curator-approved content is included
in the Runtime proposal/Assist context while run traces retain only references
and the hash. Assist chooses the case scope when it has `case_id`, otherwise the
subject scope when it has `subject_id`. For `grounding_mode=sources_only`,
personal memory is not evidence and is not sent to the answer model; only
verified document fragments are eligible. The deterministic fallback parser
does not change its decision from memory.

Runtime responses include an estimated input/output token and cost envelope
until provider-authoritative usage is available. Companion reserves the LLM
budget before a run enters `answering`, records actual returned estimates after
the call, and keeps all prompt and document content out of accounting metadata.

## Explicit non-goals

Documentary evidence is never promoted into memory automatically. It remains in
`artifactindex` and is exposed through governed Knowledge Bases; see
[Grounded knowledge and professional authority](grounded-knowledge-and-authority.md).
Curation detects deterministic conflicts but does not ask an LLM to resolve
them. PostgreSQL encryption at rest protects content; field encryption is
deferred because it would prevent lexical and vector retrieval.
