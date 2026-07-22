# Advanced governance: policies, functional RBAC and delegations

Axis v2 separates three authority layers instead of combining them into a
client-supplied permission set:

- BFF owns verified identity, tenant membership and the base
  `owner | admin | member` role.
- Nexus owns additive functional role grants, governance-policy versions,
  simulations, promotions, evaluations and approvals.
- Companion owns professional delegation: on whose behalf a Virployee may use
  a capability for a product, resource, purpose and bounded risk.

The BFF removes permission, role-grant and functional-role headers supplied by
the client. It derives tenant and actor context from the session and validates
that a role-grant recipient is an active user of that tenant before forwarding
the request to Nexus.

## Functional roles

Functional roles are fixed definitions; tenants grant them additively:

| Role | Authority |
| --- | --- |
| `policy_admin` | Create, compile, simulate and request/decide policy promotions. |
| `approver` | Decide approvals within the granted product, action, resource and risk scope. |
| `auditor` | Read policy, approval, delegation and metadata-only evaluation history. |
| `delegation_admin` | Create, review and revoke professional delegations. |
| `operator` | Read operational state and perform scoped reconciliation, job/outbox recovery, incident actions, legal holds and exports. |

Every grant is tenant-scoped, time-bounded and revocable. Optional scopes cover
`product_surface`, action patterns, resource type/reference and maximum risk.
Only owners/admins administer grants. A requester cannot decide their own
approval, and a policy-version creator or promotion requester cannot approve
that promotion. Nexus derives the effective functional roles and scopes from
its grant store for the authenticated actor; role arrays in request payloads
are ignored. Approval rows retain the capability product and metadata-only
resource coordinates so the approver grant is checked against the original
request scope.

## Governance policies

Policy artifacts contain immutable CEL versions. CEL receives safe metadata
maps only: action, internal resource reference, product, requester, authority
hashes/roles and UTC time. Arguments, conversations, documents, clinical data
and other business payloads are outside the CEL environment and outside the
Nexus audit record.

Evaluation is global rather than first-match:

1. Disabled or unknown action type closes the request.
2. Any matching enforced `deny` wins.
3. Otherwise any matching `require_approval` wins.
4. Otherwise matching `allow` applies.
5. With no match, the existing risk default applies.

Risk overrides may only raise risk. `allow` never bypasses the mandatory
approval floor for high/critical actions. An enforced CEL runtime error fails
closed; a shadow error is recorded without changing the decision.

Versions move through `draft → shadow → active → retired`. Creation compiles
CEL. Promotion requires a simulation report for the same version created less
than 24 hours earlier and an independent approver. Rollback promotes a retired
version again; history is append-only. Promotions for one artifact are
serialized so concurrent decisions cannot create two active versions.

## Delegation and execution binding

A professional delegation matches only when tenant, Virployee, exact principal,
capability pattern, product, resource, risk and validity all match. It is not
transitive and cannot be subdelegated. Changes revoke the old record and create
a new one; review metadata does not rewrite the original authority.

Nexus returns a `policy_snapshot_hash` and metadata-only match/role snapshots on
every governance check. Companion stores that hash immutably on the durable
prepared action and Nexus stores it on the approval. Immediately before a side
effect, Companion re-resolves professional authority and asks Nexus to
revalidate the original check. A changed active policy, capability manifest,
assignment or delegation invalidates the approval; an unavailable Nexus fails
closed. Shadow versions remain visible in evaluation history but are excluded
from this authority snapshot, so experimenting in shadow cannot invalidate an
already approved action. The governance product comes from the active
capability manifest rather than from client input.

## API surface

Nexus exposes these authenticated routes under `/v1`; BFF forwards the public
management subset under `/api`:

```text
GET                  /role-definitions
GET|POST             /role-grants
POST                 /role-grants/:id/revoke
POST                 /internal/authorization:check

GET|POST             /governance-policies
GET                  /governance-policies/:id
POST                 /governance-policies/:id/versions
POST                 /governance-policy-versions/:id/simulate
POST                 /governance-policy-versions/:id/promotions
GET                  /governance-policy-promotions
POST                 /governance-policy-promotions/:id/approve|reject
GET                  /governance-policy-evaluations
GET                  /governance-policy-changelog
POST                 /governance/checks/:id/revalidate

GET|POST             /operations/reconciliations
GET                  /operations/overview|jobs|incidents|slos|legal-holds|exports
POST                 /operations/jobs/:id/cancel|replay
POST                 /operations/incidents/:id/acknowledge|suppress|resolve
PUT                  /operations/slos
GET|PUT               /operations/notification-policy
POST                 /operations/legal-holds
POST                 /operations/legal-holds/:id/release
POST                 /operations/exports
POST                 /operations/exports/:id/download-token
POST                 /internal/operations/findings
```

Companion keeps delegation management nested under the Virployee, including
create, review and revoke operations. Existing tenants receive no automatically
active policy. Existing delegation rows retain their former authority with
`critical` maximum risk, unrestricted product and a resource scope pinned to
their previously linked principal.

The internal finding endpoint accepts only strict, bounded metadata and a UUID
idempotency key over the authenticated service channel. Nexus deduplicates each
delivery before changing incident occurrence/revision state. Human operational
mutations require the corresponding scoped `operator` permission and never
derive authority from caller-supplied functional-role headers.

Incident notifications are leased from a durable outbox and delivered as
metadata-only webhooks. Destinations are stored as secret references, plaintext
HTTP is rejected outside development, retries are bounded, and exhausted
deliveries enter DLQ for an explicitly authorized replay. Worker controls use a
shared persistent circuit breaker so all replicas observe `closed`, `open`,
`half_open` and `paused` consistently; reconciliation and lease recovery jobs
cannot block themselves.
