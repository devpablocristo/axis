# Job Roles API Spec

## Purpose

Job Roles define the work function a Virployee is assigned to. They are not IAM
roles and do not grant authorization.

## Domain Model

Public representation:

```json
{
  "id": "uuid",
  "tenant_id": "tenant-id",
  "name": "Sales Assistant",
  "slug": "sales-assistant",
  "mission": "Support commercial follow-up.",
  "state": "active",
  "created_at": "2026-07-02T12:00:00Z",
  "updated_at": "2026-07-02T12:00:00Z",
  "archived_at": null,
  "trashed_at": null,
  "purge_after": null
}
```

Rules:

- `name` is required.
- `slug` is server-normalized from request `slug`; if empty, it is derived from
  `name`.
- `(tenant_id, slug)` is unique.
- Lifecycle matches Virployees: active, archived, trashed, purged.

Deferred on purpose:

- `responsibilities` and `success_criteria` are not persisted in v2 yet. In v1
  they were mostly administrative metadata and did not drive runtime, evaluation
  or permissions. They should return only when a concrete consumer needs them.

## API

Base path:

```text
/v1/job-roles
```

Endpoints:

```text
GET    /v1/job-roles
POST   /v1/job-roles
GET    /v1/job-roles?lifecycle=archived
GET    /v1/job-roles?lifecycle=trash
GET    /v1/job-roles/:job_role_id
PUT    /v1/job-roles/:job_role_id
POST   /v1/job-roles/:job_role_id/archive
POST   /v1/job-roles/:job_role_id/unarchive
POST   /v1/job-roles/:job_role_id/trash
POST   /v1/job-roles/:job_role_id/restore
DELETE /v1/job-roles/:job_role_id/purge
```

Create/update request:

```json
{
  "name": "Sales Assistant",
  "mission": "Support commercial follow-up."
}
```

## Tenancy

Companion reads `X-Tenant-ID`; missing values fall back to `default` for local
development. BFF v2 validates membership before forwarding requests.
