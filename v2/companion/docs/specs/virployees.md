# Virployees API Spec

## Purpose

Companion v2 starts by modeling and managing Virployees.

A Virployee is a digital employee definition. In this first version, the API
only creates, reads, updates and manages the lifecycle of Virployees. It does
not execute work, run LLMs, assign tasks, call tools, evaluate approvals or
integrate with other Axis services.

See `virployees-concepts.md` for the design decision that separates the current
technical v2 base from the next domain core.

## Scope

In scope:

- Create Virployees.
- List active Virployees.
- List archived Virployees.
- List trashed Virployees.
- Get one Virployee.
- Update one active Virployee.
- Archive and unarchive.
- Trash and restore.
- Purge permanently.

Out of scope for this first version:

- Tenants as public API.
- Authentication and authorization.
- Tasks.
- Runtime execution.
- LLM providers.
- Memory.
- Tools and capabilities.
- Job roles.
- Profiles.
- Supervisors.
- Nexus, BFF, Console or any Axis v1 dependency.

## Architecture Rules

- `companion` is standalone inside `v2`.
- Use the Ponti-style module structure:
  - `handler`
  - `handler/dto`
  - `usecases`
  - `usecases/domain`
  - `repository`
  - `repository/models`
- Use `platform` as an external Go library.
- Do not copy code from `platform`.
- Do not create `internal/platform`.
- Use `platform/lifecycle/go/lifecycle` for transitions supported by the
  published library version.
- Use `platform/lifecycle/go/paths` for canonical route segments when available.
- Use `platform/errors/go/domainerr` for domain errors.
- Use `platform/http/gin/go` for HTTP helpers.
- Use `platform/config/go/envconfig` for config.

## Domain Model

### Virployee

Public representation:

```json
{
  "id": "uuid",
  "name": "Sales Assistant",
  "role": "sales_assistant",
  "description": "Helps with commercial follow-up.",
  "supervisor_user_id": "uuid",
  "autonomy": "A1",
  "state": "active",
  "created_at": "2026-07-02T12:00:00Z",
  "updated_at": "2026-07-02T12:00:00Z",
  "archived_at": null,
  "trashed_at": null,
  "purge_after": null
}
```

Fields:

- `id`: server-generated UUID.
- `name`: required, trimmed, non-empty.
- `role`: required, trimmed, non-empty.
- `description`: optional, trimmed.
- `supervisor_user_id`: required UUID reference to the human responsible for
  the Virployee. It is stored as an opaque reference in this version.
- `autonomy`: optional input, defaults to `A1`. Accepted values are `A0`,
  `A1`, `A2`, `A3`, `A4` and `A5`. In this version it is persisted as
  configuration only; it does not enforce runtime permissions.
- `created_at`: resource metadata; server-generated timestamp.
- `updated_at`: resource metadata; server-generated timestamp.
- `state`: derived from lifecycle metadata. It is never accepted as input.
- `archived_at`: lifecycle metadata; set when archived, otherwise `null`.
- `trashed_at`: lifecycle metadata; set when trashed, otherwise `null`.
- `purge_after`: lifecycle metadata; set by retention policy when trashed, otherwise `null`.

These fields are not audit records. Audit is the append-only event history of
who performed an action and when. Metadata/lifecycle fields describe the current
resource row.

Autonomy definitions:

The scale is cumulative: a higher level includes the lower levels. For example,
`A3` includes `A0`, `A1`, `A2` and `A3`, but does not include `A4`.

| Level | Name | Definition |
| --- | --- | --- |
| `A0` | Conversation | Can hold conversation and read contextual information, without recommending or preparing actions. |
| `A1` | Recommendation | Can read, analyze and recommend actions. |
| `A2` | Draft | Can prepare plans or executable drafts, without external side effects. |
| `A3` | Limited execution | Can execute low-risk writes that are reversible, idempotent and scoped to the tenant. |
| `A4` | Governed execution | Can attempt medium-risk actions only with prior approval or a controlled playbook. |
| `A5` | Broad autonomy | Reserved for broad multi-product autonomy; not enabled by default. |

Action classes:

Action classes are Companion domain vocabulary. They describe what kind of
action a Virployee is trying to perform. Future capabilities/runtime will map
their operations to one action class before execution. Nexus still owns
sensitive approvals and policy decisions.

| Class | Required autonomy | Approval | Enabled | Definition |
| --- | --- | --- | --- | --- |
| `observe` | `A0` | no | yes | Read context and hold conversation without recommending, drafting or executing actions. |
| `recommend` | `A1` | no | yes | Analyze context and recommend actions without preparing executable output. |
| `draft` | `A2` | no | yes | Prepare plans or executable drafts without external side effects. |
| `write_low` | `A3` | no | yes | Execute low-risk writes that are reversible, idempotent and scoped to the tenant. |
| `write_medium` | `A4` | yes | yes | Attempt medium-risk writes only through approval or a controlled playbook. |
| `write_high` | `A5` | yes | no | Reserved for high-risk or broad-impact actions; not enabled by default. |

Autonomy decisions:

Companion can evaluate an autonomy level against an action class without
executing anything. The decision is pure domain context and can later be used by
runtime, logs, traces or UI.

```json
{
  "allowed": false,
  "requires_approval": false,
  "autonomy": "A2",
  "action_class": "write_low",
  "required_autonomy": "A3",
  "reason": "A2 does not allow write_low; required A3"
}
```

Initial states:

- New Virployees start as `active`.
- `archived` Virployees are retained but excluded from active lists.
- `trashed` Virployees are reversible deletes and excluded from active and archived lists.
- `purged` Virployees are permanently deleted and do not appear in API responses.

## Lifecycle

Use `platform/lifecycle` for archive, unarchive and hard delete through the
published `@latest` API:

- `archive` uses `lifecycle.Service.SoftDelete`.
- `unarchive` uses `lifecycle.Service.Restore`.
- `purge` checks that the Virployee is trashed, then uses
  `lifecycle.Service.HardDelete`.

`trash` and restore-from-trash are implemented inside the Virployees module
until the published lifecycle library exposes those primitives.

Canonical transitions:

```text
active -> archived -> active
active|archived -> trashed -> active
trashed -> purged
```

Resource type:

```text
virployee
```

Initial lifecycle policy:

```text
AllowArchive = true
AllowTrash = true
  AllowHardDelete = true
RequireReason = false
RetentionDays = 30
```

Lifecycle action bodies:

```json
{}
```

or:

```json
{
  "reason": "No longer needed."
}
```

`reason` is optional in the first version.

If lifecycle requires an actor internally, read `X-Actor-ID`. If missing, use
`system`. `X-Actor-ID` is not authentication.

## Tenancy

Tenants are intentionally not part of the public API in this first version.

If `platform/lifecycle` or the repository needs a tenant/scope value internally,
use a fixed internal value:

```text
default
```

Do not expose `tenant_id` in requests or responses. Do not require `X-Tenant-ID`
yet. Multi-tenancy will be designed separately after the Virployee model is
clear.

## Persistence

Use Postgres from the first implementation.

### virployees

Required columns:

- `id uuid primary key`
- `tenant_id text not null default 'default'`
- `name text not null`
- `role text not null`
- `description text not null default ''`
- `supervisor_user_id uuid not null`
- `autonomy text not null default 'A1'`
- `created_at timestamptz not null`
- `updated_at timestamptz not null`
- `archived_at timestamptz null`
- `trashed_at timestamptz null`
- `purge_after timestamptz null`

Indexes:

- `(tenant_id, archived_at, trashed_at)`
- `(tenant_id, id)`

`tenant_id` is technical-only in this version.

### lifecycle audit

Do not design a separate public audit module for the first version.

If `platform/lifecycle` requires an `AuditPort`, implement the smallest internal
adapter needed inside the Virployees module. It may persist lifecycle events in a
private table or use a no-op adapter for the first implementation, but it must
not create a new top-level domain/module.

## API

Base path:

```text
/v1/virployees
```

Health:

```text
GET /healthz
GET /readyz
```

### Create

```text
POST /v1/virployees
```

Request:

```json
{
  "name": "Sales Assistant",
  "role": "sales_assistant",
  "description": "Helps with commercial follow-up.",
  "supervisor_user_id": "11111111-1111-4111-8111-111111111111",
  "autonomy": "A1"
}
```

Response: `201 Created`

```json
{
  "id": "uuid",
  "name": "Sales Assistant",
  "role": "sales_assistant",
  "description": "Helps with commercial follow-up.",
  "supervisor_user_id": "11111111-1111-4111-8111-111111111111",
  "autonomy": "A1",
  "state": "active",
  "created_at": "2026-07-02T12:00:00Z",
  "updated_at": "2026-07-02T12:00:00Z",
  "archived_at": null,
  "trashed_at": null,
  "purge_after": null
}
```

Validation:

- `name` is required.
- `role` is required.
- `supervisor_user_id` is required and must be a UUID.
- `autonomy` is optional. Empty or omitted values default to `A1`.
- `autonomy` must be one of `A0`, `A1`, `A2`, `A3`, `A4` or `A5`.
- Unknown fields should be rejected if the HTTP helper supports it without
  custom parsing.

### List Active

```text
GET /v1/virployees
```

Response: `200 OK`

```json
{
  "data": [
    {
      "id": "uuid",
      "name": "Sales Assistant",
      "role": "sales_assistant",
      "description": "Helps with commercial follow-up.",
      "supervisor_user_id": "11111111-1111-4111-8111-111111111111",
      "state": "active",
      "created_at": "2026-07-02T12:00:00Z",
      "updated_at": "2026-07-02T12:00:00Z",
      "archived_at": null,
      "trashed_at": null,
      "purge_after": null
    }
  ]
}
```

Rules:

- Include only active Virployees.
- Exclude archived and trashed Virployees.

### List Archived

```text
GET /v1/virployees/archived
```

Rules:

- Include only archived Virployees.
- Exclude active and trashed Virployees.

### List Trash

```text
GET /v1/virployees/trash
```

Rules:

- Include only trashed Virployees.
- Exclude active and archived Virployees.

### Get

```text
GET /v1/virployees/:virployee_id
```

Response: `200 OK`

Rules:

- Return active or archived Virployees.
- Return `404` for purged or missing Virployees.
- Return trashed Virployees only from trash-oriented flows, not from this
  endpoint.

### Update

```text
PUT /v1/virployees/:virployee_id
```

Request:

```json
{
  "name": "Sales Assistant",
  "role": "sales_assistant",
  "description": "Updated description.",
  "supervisor_user_id": "11111111-1111-4111-8111-111111111111",
  "autonomy": "A2"
}
```

Response: `200 OK`

Rules:

- Only active Virployees can be updated.
- Updating archived or trashed Virployees returns `409 Conflict`.
- `state`, lifecycle timestamps and `id` cannot be changed by request body.

### Archive

```text
POST /v1/virployees/:virployee_id/archive
```

Response: `204 No Content`

Rules:

- Active only.
- Uses `platform/lifecycle` soft delete.

### Unarchive

```text
POST /v1/virployees/:virployee_id/unarchive
```

Response: `204 No Content`

Rules:

- Archived only.
- Uses `platform/lifecycle` restore.

### Trash

```text
POST /v1/virployees/:virployee_id/trash
```

Response: `204 No Content`

Rules:

- Active or archived.
- Implemented inside the Virployees module until `platform/lifecycle` exposes
  trash primitives.
- Sets `purge_after` according to retention policy.

### Restore

```text
POST /v1/virployees/:virployee_id/restore
```

Response: `204 No Content`

Rules:

- Trashed only.
- Implemented inside the Virployees module until `platform/lifecycle` exposes
  restore-from-trash primitives.
- Restores to active.

### Purge

```text
DELETE /v1/virployees/:virployee_id/purge
```

Response: `204 No Content`

Rules:

- Trashed only.
- Checks `state = trashed`, then uses `platform/lifecycle` hard delete.
- Permanently deletes the Virployee.

## Errors

Use `platform/errors/go/domainerr`.

Expected mappings:

- Invalid payload: `400`
- Invalid UUID: `400`
- Missing Virployee: `404`
- Invalid lifecycle transition: `404` or `409`, following `platform/lifecycle`
  behavior.
- Archived or trashed update attempt: `409`
- Unexpected error: `500` with generic message.

Do not expose raw internal errors in HTTP responses.

## Acceptance Criteria

- `go test ./...` passes.
- API can create a Virployee and list it as active.
- Archive removes it from active list and shows it in archived list.
- Unarchive returns it to active list.
- Trash removes it from active/archived and shows it in trash.
- Restore returns it to active list.
- Purge permanently removes it.
- Public API does not require or return tenants.
- No dependency on Axis v1 services or packages.
