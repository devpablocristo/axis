# Job Roles API Spec

## Purpose

Job Roles define a reusable profession or work function, such as `Médico
clínico`. Multiple Virployees may share the same Job Role. The Job Role says
what good work looks like; it neither identifies a particular Virployee nor
assigns a patient/customer to one.

Job Roles are not IAM roles and do not grant authorization. Capabilities,
autonomy, professional policy, delegation and Nexus remain separate controls.

## Domain Model

Public representation:

```json
{
  "id": "uuid",
  "tenant_id": "tenant-id",
  "name": "Sales Assistant",
  "slug": "sales-assistant",
  "mission": "Support commercial follow-up.",
  "responsibilities": [
    {
      "title": "Follow up open opportunities",
      "description": "Keep the commercial queue current.",
      "expected_outcome": "Every opportunity has a next step.",
      "priority": 10
    }
  ],
  "success_criteria": [
    {
      "title": "Follow-up coverage",
      "description": "Open opportunities contacted within the agreed window.",
      "target_value": "95% within 2 business days",
      "priority": 10
    }
  ],
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
- `mission` explains the durable purpose of the profession.
- `responsibilities` is an ordered definition list. Every item requires a
  `title`; `description` and `expected_outcome` are optional and `priority` is a
  non-negative integer.
- `success_criteria` is an ordered definition list. Every item requires a
  `title`; `description` and `target_value` are optional and `priority` is a
  non-negative integer.
- The complete definition is exposed in Virployee Runtime Context so previews
  and runtime configuration use the same professional contract.
- Lifecycle matches Virployees: active, archived, trashed, purged.

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
  "mission": "Support commercial follow-up.",
  "responsibilities": [
    {
      "title": "Follow up open opportunities",
      "description": "Keep the commercial queue current.",
      "expected_outcome": "Every opportunity has a next step.",
      "priority": 10
    }
  ],
  "success_criteria": [
    {
      "title": "Follow-up coverage",
      "description": "Open opportunities contacted within the agreed window.",
      "target_value": "95% within 2 business days",
      "priority": 10
    }
  ]
}
```

## Workforce relationship

A routing pool points to one Job Role and contains Virployees with that same
Job Role. Stable subject assignment belongs to the routing model, not to Job
Role. See [Workforce continuity and routing](workforce-routing.md).

## Tenancy

Companion reads `X-Tenant-ID`; missing values fall back to `default` for local
development. BFF v2 validates membership before forwarding requests.
