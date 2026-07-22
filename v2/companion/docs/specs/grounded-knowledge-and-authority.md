# Grounded Knowledge and Professional Authority

## Purpose

This specification separates four concerns that must not be collapsed into a
single prompt:

- **Knowledge** supplies facts the Virployee may use.
- **Scope policy** says which topics belong to the profession or Virployee.
- **Capabilities** enumerate actions the Virployee could perform.
- **Authority** says whether it may perform one action, for whom, now.

The default for a newly created Virployee is `grounding_mode=sources_only`.
Virployees created before this change retain `general` until an administrator
changes them, avoiding invented assignments or behavior changes during the
migration.

## Knowledge Bases

A Knowledge Base is a tenant-owned, versioned collection of references to
documents already verified and indexed by the artifact pipeline. Every base is
classified as `professional` or `private`; omitted classification defaults to
`private`, including all rows that existed before classification was added.
Registration stores the artifact scope, immutable source version and SHA-256;
it never stores signed URLs, credentials or an unverified upload. The artifact
ingestion contract remains the generic boundary for direct upload and external
connectors.

Each base has one or more bindings:

| Binding | Applies to |
| --- | --- |
| `professional` | Every Virployee whose active Job Role matches `job_role_id`. |
| `virployee` | One exact Virployee. |
| `subject` | One exact Virployee and subject. |
| `case` | One exact Virployee, subject and Assist case. |

Only active bases and documents participate. Resolution first applies tenant
and binding predicates, then retrieves artifact chunks. Companion filters the
ranked results again by registered document ID, source version and SHA-256, so a
sibling document from the same artifact generation cannot enter context.

Classification is enforced in both mutation and retrieval paths:

- A `professional` base accepts only artifact subject `professional` and only
  `professional` or `virployee` bindings.
- A `private` base accepts only `subject` or `case` bindings for one exact
  subject, and every registered document must have that same artifact subject.

This prevents an administrator from accidentally publishing a patient's
artifact through a profession-wide binding. A private base bound to one
patient/case is never resolved for another, even when both are assigned to the
same Virployee.

## Sources-only answers and citations

Assist resolves context from the current tenant, Virployee, subject and optional
case. In `sources_only` mode:

1. Runtime receives the request as a question/context, plus only verified text
   fragments resolved for that work context.
2. Memory and ungrounded general knowledge are not factual evidence.
3. If no verified text exists, Companion completes the run with
   `answer_status=abstained` and “No está en las fuentes disponibles.”
4. An answered response must cite at least one supplied document identifier.
5. Companion accepts a citation only when its document ID, optional SHA-256 and
   optional locator match an actually retrieved fragment. Any unsupported,
   cross-subject or cross-tenant citation converts the answer to abstention.

Assist persists `grounding_mode`, `answer_status` and canonical validated
citations. Citation records contain source identifiers and locators, never the
source body or a signed URL. In `general` mode citations are optional and scoped
memory may be supplied, but tenant/subject/case retrieval constraints still
apply.

## Topic scope

Each Virployee has a revisioned scope policy:

- `allowed_topics`
- `prohibited_topics`
- `out_of_scope = abstain | escalate`

Prohibited topics always win. When allowed topics are configured, work outside
that set follows `out_of_scope`; an empty policy defaults safely to `abstain`.
Assist enforces the resolved policy before producing an answer, while action
execution evaluates the same professional boundary together with capability
rules.

## Versioned professional policy packs

A Professional Policy Pack is immutable by `(tenant, policy_key, version)` and
may belong to a Job Role. Resolution automatically selects the latest active
version of every pack for the Virployee's profession, plus packs explicitly
bound to that Virployee. Rules can define:

- allowed and prohibited topics;
- allowed and prohibited capability patterns;
- `abstain` or `escalate` for out-of-scope work; and
- whether a current delegation is required.

Creating a new version does not rewrite history. Scope policy, Virployee pack
bindings and delegations use optimistic revisions and emit metadata-only,
transactional authority events.

## Delegation

A delegation records on whose behalf a Virployee may act. It contains:

- a principal type and stable principal ID;
- one or more exact or wildcard capability scopes;
- product and exact resource scopes;
- a maximum risk class and stated purpose;
- `valid_from` and `valid_until`; and
- grant actor, review metadata, revision and optional revocation metadata.

Supported principal types are `person`, `organization`, `team`, `case`,
`project` and `service`. A delegation is usable only inside its validity window,
while not revoked, and only when capability, product, resource and risk all
match the proposed action. Delegations are not transitive and cannot be
subdelegated.
When policy requires delegation, absence, expiry or revocation blocks the
action. The execution request must identify the exact `principal_type` and
`principal_id`; authority selects only a delegation for that same principal and
capability. The prepared action, authority snapshot and revalidation all bind
that principal, so a delegation for one patient/customer cannot authorize work
for another.

Tenant owners/admins may manage every professional authority resource. A user
with a current, matching Nexus `delegation_admin` grant may create, review and
revoke delegations only inside that grant's scope. Other scope-policy,
policy-pack and continuity changes remain owner/admin-only. Read and write
operations remain tenant scoped.

## Action authorization and context binding

Capabilities remain action declarations, not knowledge. A side-effecting action
may proceed only when all of these checks pass:

```text
capability assigned
AND autonomy sufficient
AND professional scope/policy permits it
AND required delegation is current
AND Nexus allows it or its exact approval is valid
```

Every Assist run persists a deterministic `context_hash` over tenant,
responsible Virployee and Job Role, subject, case, continuity
assignment/version, product/assist/repository scope, grounding mode, resolved
source identifiers/hashes, the complete source-authorization snapshot and the
conversation-policy snapshot. Both assignment and source authorization are
revalidated before execution; reassignment or a binding/version change prevents
a queued or previously approved run from continuing under stale ownership.

The side-effect Execution Gate separately binds the exact prepared action,
memory context hash when used, scope-policy revision, policy-pack
IDs/versions/revisions, selected delegation/revision and the active Nexus policy
snapshot. Nexus persists the policy and authority snapshots alongside the normal
binding hash; Companion also stores the Nexus policy snapshot immutably on the
prepared action. Companion re-resolves authority and calls Nexus revalidation
immediately before execution, failing closed if any bound snapshot changed.
An action derived from an Assist run must also carry that run's `context_hash`,
so reassignment, source changes, policy changes, delegation expiry/revocation
or any other bound-context change invalidates the earlier approval.

Policy text, patient data, document content, PHI, secrets and signed URLs never
cross the Companion-to-Nexus authority contract.

MCP-originated actions add a metadata-only binding containing the selected
subject/case, continuity assignment revision, capability manifest and MCP
policy revision, authority snapshot, payload hash and stable idempotency hash.
The binding is part of the prepared-action payload and is revalidated before an
approved executor runs. Raw MCP arguments are not included in the Nexus check.

## Management API

Companion exposes these paths under `/v1`; BFF forwards them below `/api`:

```text
GET|POST             /knowledge-bases
GET|PUT               /knowledge-bases/:knowledge_base_id
POST                  /knowledge-bases/:knowledge_base_id/archive
POST                  /knowledge-bases/:knowledge_base_id/activate
GET|POST              /knowledge-bases/:knowledge_base_id/documents
POST                  /knowledge-bases/:knowledge_base_id/documents/:document_id/archive
GET|PUT               /knowledge-bases/:knowledge_base_id/bindings
GET|PUT               /virployees/:virployee_id/knowledge-bases

GET|POST              /professional-policy-packs
GET                   /professional-policy-packs/:policy_pack_id
GET|PUT               /virployees/:virployee_id/scope-policy
GET|PUT               /virployees/:virployee_id/professional-policy-packs
GET|POST              /virployees/:virployee_id/delegations
POST                  /virployees/:virployee_id/delegations/:delegation_id/revoke
POST                  /virployees/:virployee_id/delegations/:delegation_id/review
```

Knowledge Base, scope-policy and policy-pack management require an owner/admin
role resolved and forwarded by BFF. Delegation management additionally accepts
a current Nexus `delegation_admin` grant, constrained to the target delegation's
product, capability, resource and maximum risk.

The Virployee-nested Knowledge Base endpoint lists professional bases resolved
through its Job Role or direct binding. Its `PUT` enables/disables one direct
Virployee binding with `knowledge_base_id`, `expected_version` and `enabled`;
private subject/case repositories continue to be managed through their exact
base bindings.

## Acceptance invariants

- Two subjects sharing one Virployee never share subject/case memory or private
  documents.
- A source-free `sources_only` question abstains.
- A citation not present in the retrieved set is rejected.
- Expired or revoked required delegation blocks execution.
- A changed assignment or authority revision makes an older approval unusable.
- Legacy Virployees retain their prior grounding mode and receive no synthetic
  routing assignment.
