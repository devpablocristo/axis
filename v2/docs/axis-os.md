# Axis OS v2

Axis v2 is a digital work operating system. It is not a CRUD app: it defines
identity, organization ownership, workforce, runtime boundaries, and the request context used
when services collaborate.

## Services

- `bff` is the HTTP shell and control plane for the OS. It owns human identity,
  session resolution, organizations, memberships, products, and the gateway into downstream
  services. It also owns the global product-integration envelope and persistent,
  rotatable machine credentials.
- `companion` is the workforce/runtime service. It owns Job Roles, Virployees,
  work subjects, stable routing, capabilities, autonomy, scoped memory,
  knowledge bindings, professional authority, execution orchestration and the
  operational view/reconciliation of its Virployee fleet and durable work.
  Prompt content, business watchers, synthetic behavior evaluations and the
  append-only FinOps ledger remain Companion-owned.
- `nexus` is the governance service. It owns action types, additive functional
  role grants, versioned CEL policy evaluation, durable approvals and approval
  decisions, plus enterprise operations such as incidents, SLOs, legal holds
  and governed exports.
- `runtime` proposes an assigned capability UUID and schema-bound arguments.
  Its application owns `ModelPort` and `EmbeddingPort`; Vertex/Gemini and the
  deterministic development fallback are outbound adapters. Runtime never
  decides authorization or executor selection.
- `artifact-worker` runs the isolated extraction application. OCR,
  transcription, media conversion and operating-system commands are outbound
  adapters behind `ProfileExtractorPort`; the HTTP contract stays bounded and
  authenticated.
- Services communicate through HTTP. No service imports another service's
  internal packages.

## Vocabulary

- `principal` is the IAM subject that performs an action. It can be a human,
  Virployee, internal service, or background job.
- `actor` is the audit/event wording for who did something. In deployed
  environments BFF derives it from the verified Clerk session and replaces
  caller-supplied identity headers. Development mode may use `X-Actor-ID`.
- `organization` is the sole customer ownership, membership and isolation boundary.
- `product` is a child of exactly one organization and is selected by
  `product_surface` inside that organization.
- `membership` connects a principal/user to an organization and grants an organization role.
- `gateway` is the BFF boundary that validates OS context before forwarding to
  a downstream service.

## Axis Request Context

Every product invocation is represented as `axis.invocation-context.v1`:

- `principal_id`: who is acting.
- `principal_type`: trusted human, service or Virployee classification.
- `org_id`: the effective customer organization where the action happens.
- `product_surface`: the selected product owned by that organization.
- `product_id`: the stable product identity inside that organization.
- integration ID, revision and contract hash when installed;
- granted scopes; and
- `direct` or `via_orchestrator` access mode.

BFF forwards that context to Companion and Nexus with:

- `X-Org-ID`
- `X-Product-Surface`
- `X-Product-ID` / `X-Axis-Product-ID`
- `X-Actor-ID`
- `X-Axis-Forwarded-By`
- `X-Axis-Org-Role`
- `X-Axis-Integration-ID`
- `X-Axis-Integration-Version`
- `X-Axis-Integration-Hash`
- `X-Axis-Principal-Type`
- `X-Axis-Principal-ID`
- `X-Axis-Principal-Scopes`
- `X-Axis-Access-Mode`

Downstream services accept business routes only when the request also carries
the shared internal authentication token. Companion uses the same protected
channel for governance calls to Nexus. Health endpoints remain public.

## Current Scope

- Clerk sessions are verified at the BFF boundary; production cannot start in
  development identity mode or without its issuer configuration.
- In Clerk mode, Clerk is the source of truth for organizations, users and
  organization memberships. BFF application use cases depend on identity
  provider ports; the Clerk adapter reads and mutates that directory before
  synchronizing Axis's local organization/user projections. Those projections
  keep stable internal IDs and product ownership, while product records remain
  children of exactly one organization.
- Nexus evaluates `allow`, `deny` and `require_approval` using immutable CEL
  policy versions plus the risk defaults, with shadow simulation, independent
  promotion and durable binding hashes. See
  [Advanced governance](advanced-governance.md).
- Companion can execute an approved, durable `axis.prepared-action.v2` after
  revalidating its binding and authority. The action binds a canonical
  capability UUID, manifest and schema hashes, operation, arguments,
  idempotency and `executor_binding_id`. Dispatch uses only that binding;
  domain phrases and legacy capability keys never select code.
- Organization-specific executors implement `axis.connector.v1`: HTTPS outside
  development, HMAC request/response signatures, bounded schemas and sizes,
  stable invocation/idempotency keys, and result lookup after ambiguous
  timeouts. Calendar and clinical behavior are extension or explicit legacy
  adapters, not Axis core defaults.
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
- BFF owns organization membership and product ownership. Companion persists
  `org_id` on its own records and independently enforces that boundary after
  BFF resolves the request context.
- A product (machine, not a Clerk user) installs an immutable,
  topology-neutral `axis.product-integration.v3` contract under its existing
  organization-owned product. BFF owns the global pointer and hashed
  credential; a configured `IntegrationParticipant` registry projects only
  applicable functional snapshots. V2 remains immutable behind a compatibility
  translator. The credential authorizes declared entrypoints and scopes; it
  never lets the caller invent an organization, Virployee or permission.
- An installed product can call the BFF inbound edge `POST /v1/assist-runs` or
  `POST /v1/product-events`. BFF derives organization, product, integration
  revision/hash and technical principal from the credential, strips forged
  authority headers, and submits work to Companion. BFF returns `200`
  when an optional bounded synchronous wait observes completion, otherwise
  `202` with `id` and `status_url`; `GET /v1/assist-runs/:id` polls the same
  organization/virployee-scoped row. `GET /v1/assist-capabilities` exposes the current
  ingress contract and limits. Missing, suspended, incompatible or drifted
  integrations fail closed. See
  [Product integration contract](product-integration-contract.md).
- Every virployee has a tamper-evident audit ledger held by Nexus: assist runs
  and governed executions append a hash-chained, optionally HMAC-signed event
  (`POST /v1/audit/events`), chained per virployee (`chain_scope =
  <organization>/<virployee>`). The ledger is append-only at the DB level;
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
  The job dedupe scope is organization +
  product + kind + logical key, so replicas cannot process the same logical tick
  twice. V2 stores this queue in `companion_runtime_*` tables, deliberately
  separate from the differently shaped v1 operational queue when both versions
  share a database. Reconciliation re-enqueues received assist rows, safely resets stale
  pre-answer assists within a bounded recovery budget, finalizes stale answering
  runs, and recovers stale governed
  executions with the original idempotency key, and retries failed governance
  result reports. Execution completion and creation of its neutral, versioned
  governance outbox message are one Companion database transaction. A bounded dispatcher delivers
  that immutable snapshot with leases, heartbeat, exponential backoff, ten
  attempts, dead-letter and explicit replay; the Nexus HTTP sender is one
  adapter, while `nexus_report_status` remains a compatibility projection of
  the neutral outbox state. Persisted failures are stable
  error codes rather than raw errors, and operational events contain metadata only — never payloads,
  PHI, secrets, or signed URLs. Every affected business record still appends
  hash-only metadata to the virployee ledger. Scheduler and worker goroutines
  stop before each service closes its database.
- Companion exposes an organization/product-scoped operations surface for fleet health,
  reconciliation runs, durable jobs, worker controls and governance outbox
  replay. Legacy `nexus_*` fields remain response projections only during the
  v2 compatibility window.
  Reconciliation findings use stable fingerprints and carry bounded metadata
  only; they are committed with a durable outbox record and delivered
  idempotently to Nexus. Nexus folds those observations into revisioned
  incidents (`opened`, `observed` or `reopened`) and also reconciles its own
  approvals, jobs and audit chains. Functional `operator` grants scope reads,
  safe repair/replay controls, incident actions, legal holds and exports; all
  mutations remain organization-bound, authorized, version checked where applicable
  and idempotent.
- Companion and Nexus independently maintain metadata-only served-product
  projections by organization, product, area and time window. Configured
  without traffic is `idle`; insufficient information is `unknown`; contract
  drift is `blocked`; governance denials are not technical failures. BFF
  composes both projections without Nexus querying or importing Companion.
- Companion resolves versioned prompts as an immutable bundle:
  non-replaceable Axis safety base, Job Role, Profile Template and Virployee.
  A product-specific binding wins over the organization default at each level.
  New promotions require a passing synthetic evaluation for the exact artifact,
  product and snapshot hash from the last 24 hours plus an independent Nexus
  authorization. Assist persists the versions and `prompt_bundle_hash`.
- Business watchers are distinct from runtime recovery watchers. A version is
  triggered by a schedule or product event, uses an exact active/conformant read
  capability as detector, and produces deduplicated typed occurrences. Modes
  are `observe`, `propose` and `execute_if_authorized`; the latter invokes the
  ordinary ToolInvocationGate with stable idempotency. A watcher never creates a
  task plan or compensation sequence.
- FinOps is an append-only cost ledger separate from quotas. It attributes
  priced or explicitly `unpriced` consumption to organization, stable product,
  Virployee, capability and model. Budgets emit informational thresholds but
  never authorize or block work; quotas remain the control mechanism.
- Job workers share persisted circuit-breaker state across replicas. Retryable
  dependency failures open an organization/product/job-kind circuit after the bounded
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
- Additional external providers and executors are registered through adapters
  and connectors; approval SoD, break-glass and specialist orchestration remain
  enforced by Nexus and Companion.

## Stable workforce and bounded professional context

A Job Role is a reusable professional contract with mission, responsibilities
and success criteria. A Routing Pool points to one Job Role and contains active,
enabled Virployees with individual `max_active_subjects`. Resolution is
serialized per organization/pool: it returns the existing continuity assignment or
chooses the least-loaded eligible member. A full pool returns `unavailable`; an
ineligible existing member returns `reassignment_required` and is never rotated
silently. Owner/admin reassignment is version checked and append-only audited.

Work Subjects represent people, patients, organizations and teams. Explicit
`works_for`, `serves` and `reports_to` relationships state who employs and who
is served by each Virployee; organization ownership remains the storage and
authorization boundary. Assist binds its `subject_id`, optional `case_id` and
resolved assignment before work begins. See
[Workforce continuity and routing](../companion/docs/specs/workforce-routing.md).

Knowledge Bases reference only documents already verified and indexed by the
artifact pipeline. `professional` bases accept only the non-personal
`professional` artifact subject and profession/Virployee bindings; `private`
bases accept one exact subject and subject/case bindings. Resolution applies
organization/Virployee/subject/case predicates before ranking and validates document
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
organization + product + assist type + subject + entrypoint Virployee, and exactly one
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
human supervisor, an organization admin or owner creates a one-hour handoff request; the requester,
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
organization-prefixed GCS bucket, extract or preserve native media, and later index
derived chunks. The original remains authoritative; text, OCR, captions,
transcripts, tables and keyframes are versioned derivatives and never replace
it.

Conversions that need native binaries cross `ExtractionPort` into the isolated
artifact-worker container; the Companion process never shells out. The worker
application depends on `ProfileExtractorPort`, while toolchain and allowlisted
process execution are outbound adapters. It has bounded multipart input/output
and per-request temporary storage; LibreOffice, Poppler/Tesseract,
ImageMagick, FFmpeg and DCMTK remain adapter details.
Office and DICOM fail closed when the worker is unavailable. PDF and
Vertex-native image/audio/video retain the verified staged original and may add
OCR, normalized media, keyframes or transcripts without replacing it. Every
returned derivative is rebound to the original document ID and SHA-256 before
it can reach indexing or Runtime.

`artifactindex` is a separate Companion bounded context, not an extension of
Virployee memory. It chunks only verified derivatives and stores 768-dimensional
`gemini-embedding-001` vectors in pgvector together with FTS text and source
provenance. Organization, virployee, product, subject, repository generation and model
are applied as SQL predicates before ranking; retrieval combines cosine and FTS
scores. Extractor, chunker and embedding versions are part of the stored index
contract, so changing one replaces affected chunks rather than mixing versions.
Runtime owns the Vertex embedding adapter and exposes it only on the internal
authenticated surface. Document and query task types stay distinct and input
truncation is disabled so incomplete clinical evidence fails visibly.

Resource governance is owned by the `quotas` bounded context. `QuotaPort` and
`UsageLedgerPort` are backed by PostgreSQL fixed windows keyed by organization,
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
active`. Its catalog UUID is canonical; the human name is descriptive and a
technical key is only a migration alias. A normalized manifest hash binds
`executor_binding_id`, operation, input/output schemas, scopes, idempotency,
rollback, timeouts/retries, postconditions, quota areas, Secret Manager refs,
attestation and cost class. Only promoted capabilities are assignable. A
governance or manifest change invalidates conformance, activation rechecks
active organization/product quota policies, and a policy required by an active
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
selected work subject/case assignment, and applies the same organization, Job Role,
professional authority, delegation, autonomy, quota and executor checks as
`tools/call`. Writes require stable idempotency and enter the existing Execution
Gate/Nexus approval path. Nexus and the MCP audit receive metadata, hashes and
internal references only; arguments, results, conversations and documents stay
inside Companion.

Nexus owns approval separation of duties. Only forwarded human supervisors,
organization admins, or owners may decide; the requester, virployee identities and
service principals cannot approve their own work. Normal high-risk approvals
need one decision. Critical break-glass approvals need two different approvers,
a non-empty justification and later review; any rejection terminates the chain.
Every decision is an append-only row and an event in the virployee ledger.
Executor results carry a canonical HMAC-SHA256 attestation over the organization,
governance check, binding, idempotency key, status, duration, result and executor
version. Nexus verifies it before persisting an external effect, and the signed
evidence pack includes the resulting decision and attestation ledger events.

Each artifact is capped at 250 MiB, one diagnosis at 500 MiB and a product
repository at 5 GiB. Fetching is streamed through a bounded spool, verifies the
declared byte count and SHA-256, sniffs the actual MIME, and fails closed on a
corrupt, unsupported or required unreadable artifact. A binary is never
represented as empty text. Staged objects are organization/virployee/subject scoped,
carry a 24-hour expiry contract and, in production, require a dedicated GCS
bucket with CMEK. Assist states progress through `received`, `staging`,
`extracting`, `indexing`, optional `planning|consulting|synthesizing`, `answering`
and `done|failed|needs_human`; PostgreSQL and the durable job lease, rather than
a process-local goroutine, own the work.
