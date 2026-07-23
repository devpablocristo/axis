# Axis v2 hexagonal boundaries

Axis v2 uses ports owned by each application core. Concrete transports,
databases, identity providers, model providers and domain executors are
adapters selected only by the process composition root.

The dependency rule is:

```text
inbound adapter → application/core ports ← outbound adapter
                         ↑
                       wire
```

Core packages cannot import `internal/adapters`, another Axis process, Clerk,
Vertex, an external executor SDK or an operating-system command runner.
`scripts/quality/check-architecture.sh` enforces the boundary.

## Product ingress

Products call the stable BFF facade only. They never discover or address
Companion or Nexus. BFF authenticates the persisted product credential and
creates `axis.invocation-context.v1`:

- organization and canonical product UUID;
- descriptive product surface;
- integration ID, revision and contract hash;
- trusted principal type and ID;
- granted scopes; and
- `direct` or `via_orchestrator` access mode.

BFF passes this context through the application ports `StartAssist`,
`GetAssistRun`, `PublishProductEvent`, `ResolveRouting` and
`AssistCapabilities`. The Companion HTTP adapter owns downstream URLs, headers
and wire DTO translation. A missing adapter or unavailable process returns a
closed dependency error; BFF itself remains available for independent
operations and reports partial downstream availability.

Persisted product credentials are authoritative. The legacy environment
binding parser is a development-only migration escape hatch requiring
`BFF_V2_ALLOW_LEGACY_PRODUCT_API_KEYS=true`; it is never consulted for a
persisted-key prefix, after revocation, or after a repository failure.

## Product integrations

New installations use `axis.product-integration.v3`. It contains only
functional entrypoints, capability UUIDs, event schemas, governed operations,
connector bindings, scopes and limits. It contains no Axis service names or
URLs.

An `IntegrationParticipant` registry is built in the BFF composition root.
Participants project the functional contract into their own snapshots and
implement prepare, validate, activate and readiness operations. Adding a
participant changes composition, not the integration use case. The immutable
v2 contract remains readable through a compatibility translator until no
active installation depends on it.

## Capability identity and execution

`capability_id` is the canonical UUID shared by catalog, assignment, manifest,
Runtime proposal, prepared action and execution checks. `name` is the
human-facing label. `capability_key` is accepted only as a migration alias and
never selects executable code.

Runtime owns neutral `ModelPort` and `EmbeddingPort` interfaces. Model output
may propose only an assigned capability UUID and schema-bound arguments.
Companion then validates the UUID, assignment, promoted manifest, input schema,
authority and idempotency before it creates `axis.prepared-action.v2`.

The Execution Gate is deterministic. It checks assignment, autonomy, manifest
hashes, schemas, professional authority, idempotency and governance. Dispatch
uses the manifest's `executor_binding_id` and operation. Domain names and
capability phrases do not participate in dispatch.

## Connector protocol

Organization-specific executors implement `axis.connector.v1`:

- a descriptor with capability UUID, operations and input/output schemas;
- `POST /v1/invocations` with a stable invocation and idempotency key;
- `GET /v1/invocations/{id}` to resolve ambiguous timeouts;
- bounded request, response and timeout limits;
- HTTPS outside development;
- HMAC-SHA256 request and response authentication using `secret_ref`; and
- validation of organization, product, capability, operation and payload.

The same invocation ID and idempotency key are reused on retry. HTTP 4xx
responses are permanent except 408 and 429. Transport errors, 408, 429 and 5xx
are retryable. After an ambiguous timeout, Axis queries the durable invocation
before deciding whether another POST is safe.

Calendar and clinical behavior are compatibility or extension adapters, not
Axis core logic. An organization can register another connector without
modifying or recompiling the application core.

## Governance

Companion depends on a neutral governance port. `nexushttp` translates the
neutral request/result to the legacy Nexus wire contract while compatibility
is active. Nexus never calls or imports Companion.

The canonical access mode is `via_orchestrator`; `via_companion` is read only
as a legacy alias. Governance unavailability, a modified manifest, an
unassigned capability, stale authority or an invalid connector/attestation
signature always blocks execution.

Outbox rows carry a neutral destination and contract version. Nexus delivery
is one adapter. Legacy `nexus_*` response fields remain alongside
`governance_*` during the migration window.

## Runtime and artifact extraction

Vertex/Gemini and deterministic development implementations are outbound
Runtime adapters behind `ModelPort` and `EmbeddingPort`.

Artifact extraction remains an application workflow behind
`ProfileExtractorPort`. OCR, transcription, media conversion and external
commands live in the toolchain/process adapters. The worker preserves bounded
I/O, temporary isolation and fail-closed behavior when a required tool is
missing.

## Console

Console calls BFF only. Authentication is supplied by `AuthPort`; Clerk is
confined to its adapter. Product-integration editing uses the topology-neutral
v3 contract. Generic confirmations render from the action schema and submit
`prepared_action`; the Calendar v1 renderer remains isolated under the legacy
compatibility adapter until no pending v1 action exists.

## Compatibility retirement

Compatibility is removed only after all of the following are true:

- no active `axis.product-integration.v2` installation remains;
- no pending Calendar v1 prepared action remains;
- the neutral outbox has drained every legacy message; and
- backfills for product UUID, capability UUID and invocation context have
  completed and been verified.

Migrations are additive. Existing hashes and executor bindings on pending
actions are never recalculated.
