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
  "tenant_id": "tenant-id",
  "capability_key": "analizar.estudios.medicos",
  "name": "Analizar estudios médicos",
  "description": "Analiza estudios médicos y comunica sus hallazgos.",
  "required_autonomy": "A1",
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
- `tenant_id`: request context tenant; Capabilities are tenant-scoped in v2.
- `capability_key`: stable internal compatibility key, unique per tenant. The Console generates it and does not expose it as the Capability's user-facing identity.
- `name`: required, clear description of the ability, for example `Analizar estudios médicos`.
- `description`: optional human explanation.
- `required_autonomy`: required minimum Virployee autonomy for assignment.
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
- unique inside the same tenant;
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
- every ID must reference an active Capability in the same tenant.
- archived or trashed capabilities cannot be assigned.
- a Virployee can receive a Capability only when:

```text
Virployee.autonomy >= Capability.required_autonomy
```

Example: a Virployee with `autonomy = A2` can receive a Capability with
`required_autonomy = A2`, but cannot receive one with `required_autonomy = A3`.

This is configuration validation only. It does not execute anything.

## API

Companion:

```text
GET    /v1/capabilities
POST   /v1/capabilities
GET    /v1/capabilities/:capability_id
PUT    /v1/capabilities/:capability_id
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
Capabilities for the current tenant.

## Explicit Non-Goals

- Tools.
- Runtime execution.
- LLM planning.
- Manifest versions.
- OAuth scopes.
- IAM permissions.
- Nexus approvals or policies.
- Risk and side-effect fields.
- Job Role recommended capabilities.
- Global/product Capability catalog.

## Decision Summary

- Capability is a minimal Companion domain entity.
- Capability is tenant-scoped and identified by UUID.
- `name` is the primary user-facing representation and describes the ability in a clear phrase.
- `capability_key` is a generated, unique tenant-local compatibility key, not a user-facing identity.
- Capability has one `required_autonomy`.
- Virployees reference capabilities by UUID.
- Assignment validates tenant, lifecycle and autonomy compatibility.
- Manifest, Tool and Nexus integration remain later phases.
