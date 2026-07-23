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
  "capability_key": "capability.abcdefghijklmnopqrstuvwx.invoke",
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
- `capability_key`: deprecated organization-local input/output alias retained
  only while v2 consumers migrate. If omitted, Companion derives an opaque
  alias from the UUID; it never selects an executor or domain code.
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
`name`. The Console does not ask for or derive a technical key from that phrase.
The server-generated UUID is the canonical identity in assignments, manifests,
runtime proposals and prepared actions.

The compatibility API still accepts and returns `capability_key`. A caller that
continues to provide one must obey the legacy validation rules below, but new
callers omit it:

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

## Domain capability extensions

Axis core does not define clinical, agricultural, Calendar or other
domain-specific capabilities. An organization installs its capability UUIDs,
schemas and executor bindings through `axis.product-integration.v3`; a domain
executor implements `axis.connector.v1`. Input and output validation come from
the active manifest, not from branches in Assist.

Runtime proposes `capability_id + arguments`. Companion verifies the exact UUID
is active, assigned and conformant, validates arguments against the manifest,
and constructs `axis.prepared-action.v2`. Unknown, unassigned, drifted or
executor-less capabilities fail closed. The accepted run snapshots the UUID,
manifest hash, binding and schema hashes; all participate in
context/idempotency and are revalidated before execution.

`/v1/virployee-routing:resolve` includes the optional canonical capability UUID.
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

Write capabilities require governance approval, evidence, required idempotency,
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
- `capability_key` is an opaque, unique organization-local compatibility alias,
  not a user-facing or execution identity.
- Capability has one `required_autonomy`.
- Capability promotion is `draft → conformant → active` and is bound to the
  normalized manifest hash.
- Conformance validates governance, quotas, secrets and attestation before
  activation.
- Virployees reference capabilities by UUID.
- Assignment validates organization, lifecycle, promotion and autonomy compatibility.
- Tool execution and governance policy evaluation remain separate bounded contexts;
  the manifest declares the contract they must satisfy.
