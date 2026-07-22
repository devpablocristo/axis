# Axis OS v2

Axis v2 is a digital work operating system. It is not a CRUD app: it defines
identity, tenancy, workforce, runtime boundaries, and the request context used
when services collaborate.

## Services

- `bff` is the HTTP shell and control plane for the OS. It owns human identity,
  session resolution, tenancy, memberships, and the gateway into downstream
  services.
- `companion` is the workforce/runtime service. It owns Virployees and later
  their job roles, capabilities, autonomy, memory, and execution runtime.
- `nexus` is the minimum governance service. It owns action types,
  request/decision evaluation, durable approvals, and approval decisions.
- `runtime` proposes intents from natural language using an LLM (Gemini via
  Vertex AI by default, authenticated with Application Default Credentials). It
  only proposes which assigned capability an input maps to; Companion always
  decides. Without credentials it falls back to an Echo provider (no external
  calls), and Companion only consults it when pointed at it.
- Services communicate through HTTP. No service imports another service's
  internal packages.

## Vocabulary

- `principal` is the IAM subject that performs an action. It can be a human,
  Virployee, internal service, or background job.
- `actor` is the audit/event wording for who did something. In deployed
  environments BFF derives it from the verified Clerk session and replaces
  caller-supplied identity headers. Development mode may use `X-Actor-ID`.
- `tenant` is the product work context for an organization:
  `tenant = org_id + product_surface`.
- `membership` connects a principal/user to a tenant and grants a tenant role.
- `gateway` is the BFF boundary that validates OS context before forwarding to
  a downstream service.

## Axis Request Context

Every request forwarded by BFF to Companion must be resolved into:

- `principal_id`: who is acting.
- `tenant_id`: where the action is happening.
- `org_id`: effective customer organization.
- `product_surface`: product surface for the tenant.
- `membership_role`: principal role in the tenant.

BFF forwards that context to Companion and Nexus with:

- `X-Tenant-ID`
- `X-Axis-Org-ID`
- `X-Product-Surface`
- `X-Actor-ID`
- `X-Axis-Forwarded-By`
- `X-Axis-Tenant-Role`

Downstream services accept business routes only when the request also carries
the shared internal authentication token. Companion uses the same protected
channel for governance calls to Nexus. Health endpoints remain public.

## Current Scope

- Clerk sessions are verified at the BFF boundary; production cannot start in
  development identity mode or without its issuer configuration.
- Nexus is implemented as a minimal governance checkpoint: `allow`, `deny`,
  `require_approval`, durable approvals, and binding hashes.
- Companion can manually execute an approved, durable prepared action after
  validating the approval binding hash. Executors are selected per capability by
  the `COMPANION_V2_EXECUTION_MODE` set: `local` runs the calendar simulator,
  `google_calendar` creates real events in a shared Google Calendar (ADC service
  account, `COMPANION_V2_GOOGLE_CALENDAR_ID`) with the attempt's idempotency key as
  the event id. Each execution records its mode and whether it produced external
  effects; both flow through the same fail-closed, binding-checked path.
- Deleting an event (`calendar.events.delete`) is a compensating action: it runs
  through the same governed path as the create and carries its own binding hash,
  so a create's approval can never authorize the rollback. The delete is
  idempotent (an already-gone event is a success).
- Execution Gate fails closed when Nexus is unavailable or not configured.
- A virployee can also "process and respond" to input without external effects or
  approval (read/explain): the Assist usecase durably reserves an idempotent run
  in `received`, queues identifier-only work, claims it as `answering`, asks the
  runtime under the virployee's system prompt, and records `done|failed`
  (degraded when no model answered). Raw input stays in the scoped assist row;
  jobs, logs and evidence contain only identifiers, hashes and status metadata.
  This is not the action path — anything with external effects still routes
  through the Execution Gate and Nexus.
- Companion tenancy storage is deferred; BFF validates tenancy before forwarding.
- A product (machine, not a Clerk user) can call the BFF inbound edge
  `POST /v1/assist-runs` with an API key that maps to a tenant + virployee; the
  request is submitted to Companion's durable assist queue. BFF returns `200`
  when an optional bounded synchronous wait observes completion, otherwise
  `202` with `id` and `status_url`; `GET /v1/assist-runs/:id` polls the same
  tenant/virployee-scoped row. `GET /v1/assist-capabilities` exposes the current
  ingress contract and limits. This edge is separate from the human-session
  `/api` surface.
- Every virployee has a tamper-evident audit ledger held by Nexus: assist runs
  and governed executions append a hash-chained, optionally HMAC-signed event
  (`POST /v1/audit/events`), chained per virployee (`chain_scope =
  <tenant>/<virployee>`). The ledger is append-only at the DB level;
  `GET /v1/audit/virployees/:id/verify` recomputes the chain and
  `GET /v1/evidence/virployees/:id` returns a signed, exportable evidence pack
  (`?subject=` focuses it on one run). Events carry only hashes + metadata — an
  `output_hash` binds a diagnosis to its exact content, never PHI. Signing is on
  when `NEXUS_V2_SIGNING_KEY` is set; Companion emission is best-effort.
- Nexus and Companion run context-cancellable operational watchers. Nexus expires
  approvals after their configured TTL and closes the corresponding governance
  check. Each service schedules reconciliation ticks in its own durable
  PostgreSQL queue; tickers only materialize due work, while bounded workers claim it with
  `FOR UPDATE SKIP LOCKED`, renewable leases, heartbeats, retries, expired-lease
  recovery, and a replayable dead-letter state. A lost lease may resume
  `staging|extracting|indexing` from the verified staged original, but never
  replays an `answering` state because the model call may already have occurred.
  The job dedupe scope is tenant +
  product + kind + logical key, so replicas cannot process the same logical tick
  twice. Reconciliation re-enqueues received assist rows, safely resets stale
  pre-answer assists within a bounded recovery budget, finalizes stale answering
  runs, and recovers stale governed
  executions with the original idempotency key, and retries failed execution
  result reports to Nexus. Execution completion and creation of its Nexus outbox
  message are one Companion database transaction. A bounded dispatcher delivers
  that immutable snapshot with leases, heartbeat, exponential backoff, ten
  attempts, dead-letter and explicit replay; `nexus_report_status` remains a
  compatibility projection of the outbox state. Persisted failures are stable
  error codes rather than raw errors, and operational events contain metadata only — never payloads,
  PHI, secrets, or signed URLs. Every affected business record still appends
  hash-only metadata to the virployee ledger. Scheduler and worker goroutines
  stop before each service closes its database.
- Virployees remain the first workforce primitive.
- Virployee-owned memory admits every human, system and accepted-learning write
  through a curator that rejects secret material and quarantines poisoning or
  conflicts. Only active, approved, non-expired memories above the trust floor
  enter hybrid pgvector+FTS recall. Transactional `memory.index` jobs and a
  durable `memory.decay` schedule survive restarts; Runtime receives the
  approved content while traces retain safe references and hashes only.
- Policy engines, callbacks, break-glass, external providers, and tasks are
  future modules.

## Multimodal artifact ingestion boundary

Companion owns a separate `artifacts` bounded context for product documents;
Virployees consume its result but do not fetch or interpret product storage
directly. A product submission carries a stable repository generation plus a
manifest (`document_id`, subject, SHA-256, MIME and size). Signed read URLs are
transport hints only: they never participate in idempotency, the durable job
payload, logs, audit events or evidence.

The ingestion application core depends on hexagonal ports:
`ArtifactCatalogPort`, `ArtifactFetcherPort`, `MalwareScannerPort`,
`ArtifactStorePort`, `FormatAdapter`, `ExtractionPort`, `ChunkerPort`,
`EmbeddingPort`, `VectorStorePort`, `ArtifactRetrieverPort` and
`MultimodalAnswerPort`. Adapters may fetch from product URLs, scan, stage in a
tenant-prefixed GCS bucket, extract or preserve native media, and later index
derived chunks. The original remains authoritative; text, OCR, captions,
transcripts, tables and keyframes are versioned derivatives and never replace
it.

`artifactindex` is a separate Companion bounded context, not an extension of
Virployee memory. It chunks only verified derivatives and stores 768-dimensional
`gemini-embedding-001` vectors in pgvector together with FTS text and source
provenance. Tenant, virployee, product, subject, repository generation and model
are applied as SQL predicates before ranking; retrieval combines cosine and FTS
scores. Extractor, chunker and embedding versions are part of the stored index
contract, so changing one replaces affected chunks rather than mixing versions.
Runtime owns the Vertex embedding adapter and exposes it only on the internal
authenticated surface. Document and query task types stay distinct and input
truncation is disabled so incomplete clinical evidence fails visibly.

Executor credentials and attestation keys are resolved through opaque
`secret_ref` values by service-local Secret Manager adapters. Secret bytes are
held only long enough to construct a credential or signer and are never stored
in PostgreSQL, memory records, logs, jobs, audit events, or evidence. Production
fails closed when a required reference is absent; development may derive the
attestation key from the already configured internal token under a distinct
domain separator, but never persists that derived key.

Nexus owns approval separation of duties. Only forwarded human supervisors,
tenant admins, or owners may decide; the requester, virployee identities and
service principals cannot approve their own work. Normal high-risk approvals
need one decision. Critical break-glass approvals need two different approvers,
a non-empty justification and later review; any rejection terminates the chain.
Every decision is an append-only row and an event in the virployee ledger.
Executor results carry a canonical HMAC-SHA256 attestation over the tenant,
governance check, binding, idempotency key, status, duration, result and executor
version. Nexus verifies it before persisting an external effect, and the signed
evidence pack includes the resulting decision and attestation ledger events.

Each artifact is capped at 250 MiB, one diagnosis at 500 MiB and a product
repository at 5 GiB. Fetching is streamed through a bounded spool, verifies the
declared byte count and SHA-256, sniffs the actual MIME, and fails closed on a
corrupt, unsupported or required unreadable artifact. A binary is never
represented as empty text. Staged objects are tenant/virployee/subject scoped,
carry a 24-hour expiry contract and, in production, require a dedicated GCS
bucket with CMEK. Assist states progress through `received`, `staging`,
`extracting`, `indexing`, `answering` and `completed|failed`; PostgreSQL and the
durable job lease, rather than a process-local goroutine, own the work.
