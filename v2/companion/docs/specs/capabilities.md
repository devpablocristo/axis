# Capabilities Spec

## Purpose

This document defines the minimum useful Capability model for `companion` v2.

```text
Capability = reusable work ability declared by contract
```

A Capability describes what a Virployee may be configured to do. It is not an
IAM permission, not a Tool, not a runtime execution contract and not a Nexus
approval or policy.

## Model

Public representation:

```json
{
  "id": "uuid",
  "org_id": "org-id",
  "capability_key": "analizar.estudios.medicos",
  "name": "Analizar estudios médicos",
  "description": "Analiza estudios médicos y comunica sus hallazgos.",
  "required_autonomy": "A1",
  "promotion_state": "draft",
  "manifest_hash": "",
  "state": "active",
  "created_at": "2026-07-03T12:00:00Z",
  "updated_at": "2026-07-03T12:00:00Z",
  "archived_at": null,
  "trashed_at": null,
  "purge_after": null
}
```

Fields:

- `id`: server-generated UUID and the relationship identity.
- `org_id`: request context organization; Capabilities are organization-scoped in v2.
- `capability_key`: stable internal compatibility key, unique per organization. The Console generates it and does not expose it as the Capability's user-facing identity.
- `name`: required, clear description of the ability, for example `Analizar estudios médicos`.
- `description`: optional human explanation.
- `required_autonomy`: required minimum Virployee autonomy for assignment.
- `promotion_state`: release gate independent from resource lifecycle. New
  capabilities start `draft`, pass to `conformant` after validation and become
  assignable only when explicitly promoted to `active`.
- `manifest`: versioned execution contract for product surface, schemas,
  scopes, idempotency, rollback, timeout/retry, postconditions, quota areas,
  secret references, attestation and cost class.
- `manifest_hash`: SHA-256 of the normalized manifest. `conformed_hash` must
  match it before activation; any manifest or governance-contract change
  clears conformance and returns the capability to `draft`.
- lifecycle fields: same active, archived, trash and purge semantics used by
  v2 resources.

`required_autonomy` values:

```text
A0 Conversation
A1 Recommendation
A2 Draft
A3 Limited execution
A4 Governed execution
A5 Broad autonomy
```

There is no default for `required_autonomy`; the creator must choose it.

## User-facing Capability and internal key

People create, find and assign a Capability by its clear ability phrase in
`name`. The Console must not ask them to invent or understand a technical key.
It generates `capability_key` from the phrase for API and runtime compatibility.

The direct API contract still accepts and returns `capability_key`. Its internal
rules are:

`capability_key` rules:

- exactly three dot-separated segments: `domain.resource.action`;
- each segment uses lowercase letters only;
- `ñ` is allowed;
- numbers, spaces, hyphens, underscores and accents are not allowed;
- unique inside the same organization;
- describes the work ability, not the tool implementation.

Examples:

```text
billing.invoice.read
crm.contact.summarize
soporte.ticket.responder
```

Invalid examples:

```text
billing.read
calendar.events.read.today
Calendar.Events.Read
support.ticket.draft_reply
crm.contact.read2
```

## Virployee Integration

Virployees reference Capabilities by UUID:

```json
{
  "capability_ids": [
    "11111111-1111-4111-8111-111111111111"
  ]
}
```

Rules:

- `capability_ids` is optional on create and update.
- missing or empty means no configured capabilities.
- duplicate IDs are normalized away.
- every ID must reference a lifecycle-active, promotion-`active` Capability in
  the same organization.
- archived or trashed capabilities cannot be assigned.
- a Virployee can receive a Capability only when:

```text
Virployee.autonomy >= Capability.required_autonomy
```

Example: a Virployee with `autonomy = A2` can receive a Capability with
`required_autonomy = A2`, but cannot receive one with `required_autonomy = A3`.

This is configuration validation only. It does not execute anything.

## Clinical read capabilities

Axis defines two organization-scoped, product-neutral keys. The consumer must provide
its own `product_surface` when it installs the manifest:

- `clinical.records.search`: A0, read-only, medium risk, evidence required,
  30-second timeout, one attempt, inbound/embeddings quotas.
- `clinical.timeline.build`: A1, read-only, medium risk, evidence required,
  120-second timeout, one attempt, inbound/LLM quotas.

Their input and output schemas set `additionalProperties=false`. Search returns
bounded excerpts, scores and canonical document/source/hash/locator references;
its opaque cursor is bound to organization, Virployee, subject, case, product,
repository generation, query and manifest hash. Timeline is a projection, not
a persisted clinical entity. It returns ordered events, coverage and canonical
references, with `completed|partial|abstained` status.

Assist accepts a nullable `capability_key` for backward compatibility. When it
is present, only these registered read executors are valid: write, unknown or
executor-less capabilities fail closed. The accepted run snapshots the
canonical key and manifest hash; both participate in context/idempotency and
are checked again before execution. Axis does not translate product-owned
aliases; a consumer adapter must send the canonical capability key.

`/v1/virployee-routing:resolve` includes the optional canonical capability key.
Existing assignments become `reassignment_required` when their member no
longer has an active/conformant assignment or enough autonomy; new candidates
are filtered by the same predicates.

## Conformance and promotion

The conformance gate is deterministic and fail-closed. It validates:

- input and output JSON schemas;
- declared scopes and side-effect governance;
- evidence, idempotency and rollback policy;
- bounded timeout, retries and postconditions;
- active PostgreSQL quota policies for every declared area under
  `(org_id, product_surface, area)`;
- Secret Manager references only (never inline credentials); and
- executor attestation for write capabilities.

Write capabilities require Nexus approval, evidence, required idempotency,
manual or automatic rollback, an executor quota, postconditions and signed
attestation. All capabilities require an inbound quota and an explicit cost
class. Conformance stores a structured report and the exact manifest hash.
Activation rechecks the contract and quotas so a disabled policy cannot be
promoted accidentally. Existing rows are migrated as active for compatibility;
once their governed contract changes they enter the same draft gate as new
rows.

## API

Companion:

```text
GET    /v1/capabilities
POST   /v1/capabilities
GET    /v1/capabilities/:capability_id
PUT    /v1/capabilities/:capability_id
PUT    /v1/capabilities/:capability_id/manifest
POST   /v1/capabilities/:capability_id/conform
POST   /v1/capabilities/:capability_id/activate
POST   /v1/capabilities/:capability_id/archive
POST   /v1/capabilities/:capability_id/unarchive
POST   /v1/capabilities/:capability_id/trash
POST   /v1/capabilities/:capability_id/restore
DELETE /v1/capabilities/:capability_id/purge
```

Virployees:

```text
POST /v1/virployees
PUT  /v1/virployees/:virployee_id
```

BFF and Console should only forward, display and select active Companion
Capabilities for the current organization.

## Explicit Non-Goals

- Tools.
- Runtime execution.
- LLM planning.
- IAM permissions.
- Job Role recommended capabilities.
- Global/product Capability catalog.
- Persisted clinical timeline tables or consumer-owned clinical engines.

## Decision Summary

- Capability is a minimal Companion domain entity.
- Capability is organization-scoped and identified by UUID.
- `name` is the primary user-facing representation and describes the ability in a clear phrase.
- `capability_key` is a generated, unique organization-local compatibility key, not a user-facing identity.
- Capability has one `required_autonomy`.
- Capability promotion is `draft → conformant → active` and is bound to the
  normalized manifest hash.
- Conformance validates governance, quotas, secrets and attestation before
  activation.
- Virployees reference capabilities by UUID.
- Assignment validates organization, lifecycle, promotion and autonomy compatibility.
- Tool execution and Nexus policy evaluation remain separate bounded contexts;
  the manifest declares the contract they must satisfy.
