# Workforce Continuity and Routing

## Decision

Axis models profession, worker and served party as different concepts:

- A **Job Role** is the reusable profession: `Médico clínico`, `Secretaria` or
  `Analista contable`.
- A **Virployee** is one digital worker configured with that Job Role.
- A **Work Subject** is the person, patient, company or team for whom work is
  done.
- A **Routing Pool** groups interchangeable Virployees of one Job Role for the
  purpose of initial assignment.
- A **Continuity Assignment** fixes one subject to one Virployee within a pool.

There can be many Virployees for one profession. One Virployee can serve many
subjects until its configured capacity is reached. Two patients may therefore
share a Virployee, but never share private memory, documents or case context.

## Work subjects and employer relationships

`work_subjects` are organization-owned identities with an optional product-owned
`external_ref`. Supported kinds are `person`, `organization`, `team` and
`patient`. Archive retains the identity and history while excluding it from new
capacity calculations.

A Virployee has explicit relationships to subjects:

- `works_for`: the organization or person employing the Virployee.
- `serves`: a party the Virployee serves.
- `reports_to`: an operational reporting relationship.

Replacing relationships is atomic and requires exactly one primary
`works_for`. The organization remains the storage and authorization boundary; the
primary employer describes whom the Virployee works for and does not replace
organization ownership.

## Pools and capacity

A Routing Pool belongs to one active Job Role. A member is operationally
eligible only when:

- the pool, Virployee and Work Subject are active;
- the member is enabled;
- the Virployee's Job Role matches the pool's Job Role.

Admission of a new subject or explicit reassignment additionally requires
`active_subjects < max_active_subjects`. Capacity does not evict or silently
rotate an existing subject; lowering a limit stops further admission until the
member is below its limit.

`max_active_subjects` is positive and configured per member, allowing two
Virployees in the same profession to have different capacity.

## Stable resolution

The continuity key is:

```text
organization + routing_pool + work_subject -> virployee
```

`POST /v1/virployee-routing/resolve` behaves as follows:

1. If an assignment exists and its member remains eligible, return it with
   `status=assigned` and `created=false`.
2. If an assignment exists but its member is no longer eligible, return that
   same assignment with `status=reassignment_required`. Never rotate silently.
3. If no assignment exists, choose an operationally eligible member below
   capacity with the lowest active subject count. Creation time and Virployee ID
   are deterministic tie-breakers.
4. If every member is at capacity, return `status=unavailable` without creating
   an assignment.

Callers may also supply a canonical `capability_id` UUID. In that case both an
existing assignee and every new candidate must have that active, conformant
capability assigned and sufficient autonomy. Axis accepts only canonical
capability UUIDs; `capability_key` is only a compatibility alias and never
selects an assignment or executor. A capability mismatch returns
`reassignment_required` for an existing
assignment or `unavailable` for a new one; it never rotates the subject
silently.

Resolution serializes by organization and pool and the database enforces one row per
`organization + pool + subject`. Concurrent resolves therefore converge on a single
assignment.

Reassignment is an explicit owner/admin operation. It requires the current
assignment version, a safe reason code and an eligible target below capacity.
The row keeps its identity and `assigned_at`; its Virployee and version change,
and an append-only event records previous/new Virployee, actor, reason and
version. Subject/case history is retained. Any authorization bound to the old
assignment must no longer execute.

## Assist contract

Assist receives `subject_id`, an optional `case_id`, and the resolved continuity
`assignment_id`. Companion verifies that organization, pool, subject, assignment and
responsible Virployee agree before work is accepted and snapshots the current
assignment version. It revalidates that version before processing durable work.
A case can narrow context further, but cannot widen the subject boundary.

Machine ingress also carries BFF-derived `product_id`, integration revision and
contract hash. A credential authorizes contract-declared pools or direct
entrypoints but never embeds an implicit subject-to-Virployee choice. Product
headers supplied by the caller are discarded before routing.

Existing Virployees keep working without fabricated subjects, pools or
assignments. Continuity routing applies only when the caller supplies the new
work context.

## API

Companion exposes the following under `/v1`; BFF forwards the same paths below
`/api` after resolving organization and actor context:

```text
GET|POST            /work-subjects
GET|PUT              /work-subjects/:subject_id
POST                 /work-subjects/:subject_id/archive
POST                 /work-subjects/:subject_id/unarchive

GET|POST             /routing-pools
GET|PUT               /routing-pools/:pool_id
POST                  /routing-pools/:pool_id/archive
POST                  /routing-pools/:pool_id/unarchive
GET                   /routing-pools/:pool_id/members
PUT                   /routing-pools/:pool_id/members/:virployee_id

GET|PUT               /virployees/:virployee_id/relationships
POST                  /virployee-routing/resolve
GET                   /virployee-routing/assignments
POST                  /virployee-routing/assignments/:assignment_id/reassign
```

All database lookups include `org_id`; cross-organization identifiers are rejected
as missing or invalid rather than resolved globally.

## Acceptance invariants

- Repeated resolution for one pool and subject returns the same Virployee.
- Concurrent resolution creates one assignment.
- Capacity blocks new subjects but never evicts an existing assignment.
- An inactive assigned Virployee produces `reassignment_required`.
- Reassignment is explicit, version checked and audited.
- Two subjects assigned to the same Virployee remain isolated in memory,
  knowledge and Assist context.
