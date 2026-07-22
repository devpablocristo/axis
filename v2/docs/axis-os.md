# Axis OS v2

Axis v2 is a digital work operating system. It is not a CRUD app: it defines
identity, tenancy, workforce, runtime boundaries, and the request context used
when services collaborate.

## Services

- `bff` is the HTTP shell and control plane for the OS. It owns human identity,
  session resolution, tenancy, memberships, and the gateway into downstream
  services.
- `companion` is the workforce/runtime service. It owns Job Roles, Virployees,
  work subjects, stable routing, capabilities, autonomy, scoped memory,
  knowledge bindings, professional authority, execution orchestration and the
  operational view/reconciliation of its Virployee fleet and durable work.
- `nexus` is the governance service. It owns action types, additive functional
  role grants, versioned CEL policy evaluation, durable approvals and approval
  decisions, plus enterprise operations such as incidents, SLOs, legal holds
  and governed exports.
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
- Nexus evaluates `allow`, `deny` and `require_approval` using immutable CEL
  policy versions plus the risk defaults, with shadow simulation, independent
  promotion and durable binding hashes. See
  [Advanced governance](advanced-governance.md).
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
  in `received`, queues identifier-only work, advances it through persisted
  processing states, asks the runtime under the virployee's system prompt, and
  records `done|failed|needs_human`
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
- Companion exposes a tenant/product-scoped operations surface for fleet health,
  reconciliation runs, durable jobs, worker controls and Nexus outbox replay.
  Reconciliation findings use stable fingerprints and carry bounded metadata
  only; they are committed with a durable outbox record and delivered
  idempotently to Nexus. Nexus folds those observations into revisioned
  incidents (`opened`, `observed` or `reopened`) and also reconciles its own
  approvals, jobs and audit chains. Functional `operator` grants scope reads,
  safe repair/replay controls, incident actions, legal holds and exports; all
  mutations remain tenant-bound, authorized, version checked where applicable
  and idempotent.
- Job workers share persisted circuit-breaker state across replicas. Retryable
  dependency failures open a tenant/product/job-kind circuit after the bounded
  threshold; one half-open probe decides recovery, while protected
  reconciliation and lease-recovery jobs remain runnable. Incident webhooks
  are metadata-only, resolve destinations through secret references, and use a
  leased retry/DLQ outbox. Export jobs publish only complete, hash-manifested
  artifacts; narrow Virployee, subject, case or audit scopes may select only
  categories with an exact scoped query, and one-use download tokens never
  reveal storage URIs.
- Workforce separates reusable professions (Job Roles), individual
  Virployees, served parties (Work Subjects), pools and continuity assignments.
  Several Virployees may share a profession; one Virployee may serve several
  subjects up to its member-specific capacity, while each pool/subject pair
  keeps one stable assignment.
- Virployee-owned memory admits every human, system and accepted-learning write
  through a curator that rejects secret material and quarantines poisoning or
  conflicts. Memory is nested into Virployee-global, exact-subject and
  exact-case scopes. Only active, approved, non-expired memories above the trust
  floor enter hybrid pgvector+FTS recall. Transactional `memory.index` jobs and
  a durable `memory.decay` schedule survive restarts; Runtime receives approved
  content only in the resolved scope while traces retain safe references and
  hashes.
- Additional external providers and a general-purpose task engine remain future
  modules; approval SoD, break-glass and specialist orchestration are already
  enforced by Nexus and Companion.

## Stable workforce and bounded professional context

A Job Role is a reusable professional contract with mission, responsibilities
and success criteria. A Routing Pool points to one Job Role and contains active,
enabled Virployees with individual `max_active_subjects`. Resolution is
serialized per tenant/pool: it returns the existing continuity assignment or
chooses the least-loaded eligible member. A full pool returns `unavailable`; an
ineligible existing member returns `reassignment_required` and is never rotated
silently. Owner/admin reassignment is version checked and append-only audited.

Work Subjects represent people, patients, organizations and teams. Explicit
`works_for`, `serves` and `reports_to` relationships state who employs and who
is served by each Virployee; tenant ownership remains the storage and
authorization boundary. Assist binds its `subject_id`, optional `case_id` and
resolved assignment before work begins. See
[Workforce continuity and routing](../companion/docs/specs/workforce-routing.md).

Knowledge Bases reference only documents already verified and indexed by the
artifact pipeline. `professional` bases accept only the non-personal
`professional` artifact subject and profession/Virployee bindings; `private`
bases accept one exact subject and subject/case bindings. Resolution applies
tenant/Virployee/subject/case predicates before ranking and validates document
identity, source version and SHA-256 again after ranking. Newly created
Virployees default to `sources_only`: an answer needs retrieved text and at
least one citation that Companion validates against the actual fragments,
otherwise it abstains.

Professional scope policies define allowed/prohibited topics and
`abstain|escalate`. Versioned policy packs contribute topic and capability
rules, and delegations state the exact principal, product, resource, purpose and
maximum risk on whose behalf a Virployee may use matching capabilities during a
bounded time window. The Execution Gate
binds that principal and requires an assigned capability, sufficient autonomy,
applicable professional policy, any required current delegation, and Nexus
governance. Assist persists a `context_hash` over work subject, case,
continuity assignment/version, sources and conversation policy. The Execution
Gate binds memory and professional-authority revisions; an action derived from
Assist must also bind that Assist context. Companion revalidates assignment and
authority before processing or an external effect. See
[Grounded knowledge and professional authority](../companion/docs/specs/grounded-knowledge-and-authority.md).

## Governed specialist orchestration

Companion models a product interaction as a durable assist case. The case key is
tenant + product + assist type + subject + entrypoint Virployee, and exactly one
Virployee owns the final response at a time. An assist run snapshots that owner
and an optimistic `ownership_version`; synthesis and completion use that version
as a compare-and-swap guard so a stale worker cannot publish after a handoff.

An orchestration policy is scoped to the same product, assist type and entrypoint
and rolls out as `disabled → shadow → active`. Its selector and synthesis
capabilities must be active and assigned. The selector receives only allowlisted,
namespaced specialty codes from `companion_specialist_routes`; model output can
never choose a Virployee or capability ID. Go resolves each code and enforces a
maximum fan-out of three, depth one, no self/cycle, one bounded schema repair and
the policy deadlines. A decision is one of `direct`, `consult` or `needs_human`.
The durable plan snapshots the policy version and output schema, so an admin
change cannot alter an in-flight synthesis.

Specialist consultations are advisory child records of the root run. They reuse
the already staged and indexed corpus through the artifact ports, never re-fetch
a product signed URL or create a second corpus. PostgreSQL creates the plan,
consultation rows and initial `assist.specialist.consult` jobs in one transaction.
Durable workers claim them with leases and retries; reconciliation recreates any
missing consultation, synthesis or timeout work. Required consultation failure
fails the orchestration, while advisory failure is retained as a limitation for
the single owner to synthesize. Known failures return to the queue for bounded
retry; an ambiguous lease loss never replays the model call and instead fails
safely for reconciliation. `planning`, `consulting`, `synthesizing` and
`needs_human` are first-class persisted assist states.

Ownership transfer is explicit rather than an LLM tool call. The current owner's
human supervisor, a tenant admin or owner creates a one-hour handoff request; the requester,
Virployees and service principals cannot decide it, and a target-side authorized
human accepts or rejects using the current version. Acceptance atomically changes
the case owner and every non-terminal run's responsible Virployee/version. A
durable expiry job closes abandoned requests. `needs_human` creates a claimable
review item which an authorized human resolves with a coded outcome; free-text
notes stay in Companion and only their hashes enter audit.

The BFF forwards the human control plane at `/api/assist-cases`,
`/api/orchestration-policies`, `/api/specialist-routes`, `/api/handoffs` and
`/api/human-reviews`; the Console `Coordination` page exposes those operations.
Product callers keep the compatible `/v1/assist-runs` contract and receive
`case_id`, `responsible_virployee_id` and an orchestration summary. A terminal
`needs_human` is a traceable `200`, not infrastructure unavailability.

Selector, each specialist call and synthesis reserve/report LLM quota under
separate idempotency keys. Coordination events contain IDs, codes, hashes,
versions, model metadata and status only. Each participating Virployee retains
its independent Nexus hash chain for the root-run subject; a focused evidence
pack verifies the requested chain and includes other verified chains as
`linked_chains` instead of flattening or re-hashing them.

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

Conversions that need native binaries cross `ExtractionPort` into the isolated
artifact-worker container; the Companion process never shells out. The worker
has bounded multipart input/output and per-request temporary storage, and owns
the LibreOffice, Poppler/Tesseract, ImageMagick, FFmpeg and DCMTK adapters.
Office and DICOM fail closed when the worker is unavailable. PDF and
Vertex-native image/audio/video retain the verified staged original and may add
OCR, normalized media, keyframes or transcripts without replacing it. Every
returned derivative is rebound to the original document ID and SHA-256 before
it can reach indexing or Runtime.

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

Resource governance is owned by the `quotas` bounded context. `QuotaPort` and
`UsageLedgerPort` are backed by PostgreSQL fixed windows keyed by tenant,
product surface and area. An idempotency key makes retries free of duplicate
charges; one atomic statement enforces request and unit ceilings across all
replicas. Inbound assists, artifact bytes, LLM work, embeddings and external
executors reserve capacity before work begins. Denials return `429` with
`Retry-After`; production has no implicit unlimited policy. The append-only
usage ledger stores quantities, model/cost metadata and operational subject
identifiers, never prompts, artifact content, vectors, signed URLs or secrets.
The reservation also covers Runtime proposal and learning-enrichment calls;
quota denial happens before the model and optional enrichment degrades to its
deterministic analyzer without spending tokens.

Capability release is a separate promotion lifecycle: `draft → conformant →
active`. A normalized manifest hash binds schemas, scopes, idempotency,
rollback, timeouts/retries, postconditions, quota areas, Secret Manager refs,
attestation and cost class. Only promoted capabilities are assignable. A
governance or manifest change invalidates conformance, activation rechecks
active tenant/product quota policies, and a policy required by an active
capability cannot be disabled.

Executor credentials and attestation keys are resolved through opaque
`secret_ref` values by service-local Secret Manager adapters. Secret bytes are
held only long enough to construct a credential or signer and are never stored
in PostgreSQL, memory records, logs, jobs, audit events, or evidence. Production
fails closed when a required reference is absent; development may derive the
attestation key from the already configured internal token under a distinct
domain separator, but never persists that derived key.

Companion's MCP surface is a governed facade over promoted capabilities, not a
second tool registry. `tools/list` is contextual to an active Virployee and its
selected work subject/case assignment, and applies the same tenant, Job Role,
professional authority, delegation, autonomy, quota and executor checks as
`tools/call`. Writes require stable idempotency and enter the existing Execution
Gate/Nexus approval path. Nexus and the MCP audit receive metadata, hashes and
internal references only; arguments, results, conversations and documents stay
inside Companion.

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
`extracting`, `indexing`, optional `planning|consulting|synthesizing`, `answering`
and `done|failed|needs_human`; PostgreSQL and the durable job lease, rather than
a process-local goroutine, own the work.
