# Virployees Concept Decision Matrix

## Purpose

This document decides which concepts belong to the next Virployee model in
`companion`.

The first v2 API was a good technical base, but `name + role + description`
was not enough domain to represent a digital employee. A Virployee must have
identity, human responsibility, a work function and minimum execution safety.

This is a design decision document only. It does not change the current REST
API by itself.

## Classification Rules

Concepts are classified as:

- `core`: belongs in the Virployee model now.
- `deferred-core`: essential for a real Virployee, but blocked on another
  module or design.
- `metadata`: useful resource data, but not what makes it a Virployee.
- `out`: does not belong inside Virployee.

Evaluation criteria:

- Identity: needed to recognize or administer the Virployee.
- Work: defines what job it performs or what work it can receive.
- Runtime IA: changes how the Virployee acts.
- Security: limits autonomy, capability, access or human responsibility.
- v2 independence: can exist in standalone `companion` without importing
  Axis v1.

## Matrix

| Concept | Identity | Work | Runtime IA | Security | v2 independence | Classification | Decision |
|---|---:|---:|---:|---:|---:|---|---|
| `id` | yes | no | no | no | yes | `core` | Server-generated UUID. Public identity of the Virployee. |
| `name` | yes | no | no | no | yes | `core` | Required in `POST`. Human-readable display name. |
| `role` | partial | partial | no | no | yes | `out` | Removed from the new public Virployee API. `job_role_id` is the work-function reference. |
| `description` | partial | partial | no | no | yes | `metadata` | Optional note for humans. It must not carry mission, responsibilities or permissions. |
| `supervisor_user_id` | yes | no | no | yes | yes | `core` | Required opaque string in v2.1. It names the responsible human, not the creator and not auth. |
| `status` | yes | yes | yes | yes | yes | `core` | Add operational status separate from lifecycle. Values: `draft`, `active`, `disabled`, `suspended`, `error`. Default `draft`. |
| lifecycle `state` | no | no | no | partial | yes | `metadata` | Keep as technical lifecycle derived from timestamps: `active`, `archived`, `trashed`. Do not use it as operational readiness. |
| `job_role_id` | yes | yes | partial | partial | yes | `core` | Required UUID in v2.1. Replaces `role` as the work-function reference and must point to an active Job Role in the same tenant. |
| `profile_template_id` | no | no | yes | yes | yes | `core` | Required live reference to an active Profile Template. Editing the template changes the expected behavior of Virployees that use it. |
| `virployee_profile` | no | no | yes | yes | partial | `out` | Do not store a snapshot in the Virployee CRUD model now. Exact prompt/config snapshots belong later in runtime or audit logs. |
| `autonomy` | no | partial | yes | yes | yes | `core` | Optional in `POST`, default `A1`. Values: `A0` to `A5`. This is the minimum runtime safety control. |
| `capability_ids` | no | yes | yes | yes | partial | `deferred-core` | Essential later, but wait for Capability design. Do not store opaque capability lists before the registry contract is clear. |
| governed memory | no | partial | yes | yes | yes | `core` | Implemented as records scoped by tenant and Virployee; it is not a `memory_id` field on Virployee. |
| `created_at` | no | no | no | no | yes | `metadata` | Server-generated resource metadata. |
| `updated_at` | no | no | no | no | yes | `metadata` | Server-generated resource metadata. |
| `archived_at` | no | no | no | partial | yes | `metadata` | Lifecycle metadata. |
| `trashed_at` | no | no | no | partial | yes | `metadata` | Lifecycle metadata. |
| `purge_after` | no | no | no | partial | yes | `metadata` | Lifecycle retention metadata. |
| `version` | no | no | no | partial | yes | `metadata` | Useful for concurrency later, not part of the domain identity. |
| tasks | no | no | no | no | yes | `out` | Tasks point to Virployees; they do not live inside the Virployee. |
| handoffs | no | no | no | no | yes | `out` | Handoffs point from/to Virployees; they are their own workflow. |
| watchers | no | no | no | no | yes | `out` | Watchers may assign work to a Virployee, but are not part of it. |
| audit | no | no | no | yes | yes | `out` | Audit records changes about a Virployee. It is append-only history, not the Virployee core. |

## Virployee Core v2.1 Proposal

The next domain model should be:

```text
Virployee
- id: UUID
- name: string
- supervisor_user_id: string
- status: VirployeeStatus
- job_role_id: UUID
- profile_template_id: UUID
- autonomy: AutonomyLevel

Metadata
- description: string
- lifecycle_state: active | archived | trashed
- created_at: timestamp
- updated_at: timestamp
- archived_at: timestamp | null
- trashed_at: timestamp | null
- purge_after: timestamp | null
```

`role` does not survive as a domain concept in new payloads. New design uses
`job_role_id`.

`status` and lifecycle are separate:

- `status` answers whether the Virployee can operate.
- lifecycle answers whether the resource is active, archived or trashed.

## v2.1 API Shape

Suggested `POST /v1/virployees` body:

```json
{
  "name": "Billing Employee",
  "supervisor_user_id": "user_123",
  "job_role_id": "11111111-1111-4111-8111-111111111111",
  "autonomy": "A1",
  "description": "Handles billing follow-up."
}
```

Required request fields:

- `name`
- `supervisor_user_id`
- `job_role_id`

Optional request fields:

- `autonomy`, default `A1`
- `description`

Server-generated or server-derived fields:

- `id`
- `status`, default `draft`
- lifecycle state and lifecycle timestamps
- `created_at`
- `updated_at`

References stored as opaque values in v2.1:

- `supervisor_user_id`: opaque string; no auth or user module yet.
- `job_role_id`: UUID; must reference an active Job Role in the same tenant.

Memory is intentionally not stored as `memory_id` on the Virployee. The
standalone `memories` module owns multiple governed records scoped by tenant and
Virployee and supplies safe references to runtime contracts.

## Explicit Non-Goals

- Do not import or depend on Axis v1.
- Do not design public tenants, orgs or product surfaces in this step.
- Do not add tasks or LLM providers to the Virployee module.
- Do not treat `job_role_id` as authorization. Permissions and approvals remain
  separate concerns.

## Decision Summary

- `role` is not the final Virployee work model; `job_role_id` replaces it.
- `supervisor_user_id` entered the v2 API as an opaque string reference to the
  responsible human.
- `autonomy` enters the core now with safe default `A1`.
- Profile Templates are reusable catalog records; Virployees reference them
  directly by `profile_template_id`.
- Runtime/audit snapshots are deferred until execution exists.
- Governed memory is a separate one-to-many module, not a Virployee field.
- Lifecycle metadata remains technical metadata, separate from operational
  `status`.
