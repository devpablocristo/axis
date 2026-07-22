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
  recovery, and a replayable dead-letter state. The job dedupe scope is tenant +
  product + kind + logical key, so replicas cannot process the same logical tick
  twice. Reconciliation re-enqueues received assist rows, finalizes stale
  answering runs, recovers stale governed
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
- Virployee-owned lexical memory supports controlled CRUD, recall, lifecycle,
  audit hashes, and safe references in runtime traces.
- Policy engines, callbacks, break-glass, external providers, and tasks are
  future modules.
