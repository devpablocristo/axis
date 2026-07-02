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
- Services communicate through HTTP. No service imports another service's
  internal packages.

## Vocabulary

- `principal` is the IAM subject that performs an action. It can be a human,
  Virployee, internal service, or background job.
- `actor` is the audit/event wording for who did something. Public/dev headers
  can still use `X-Actor-ID`, but BFF maps that value to `principal_id`
  internally.
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

BFF currently forwards that context to Companion with:

- `X-Tenant-ID`
- `X-Axis-Org-ID`
- `X-Product-Surface`
- `X-Actor-ID`
- `X-Axis-Forwarded-By`

## Current Scope

- Clerk integration is deferred.
- Governance/Nexus is deferred.
- Companion tenancy storage is deferred; BFF validates tenancy before forwarding.
- Virployees remain the first workforce primitive.
- Runtime, tasks, capabilities, autonomy, and memory are future Companion
  modules.
