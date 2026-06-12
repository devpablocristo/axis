# ADR 0001: Product-Owned Knowledge Across Nexus and Companion

Status: accepted

## Context

Axis hosts shared services used by multiple products. Some products need to
store and run deterministic rules over product facts, and some need AI
assistance for user-facing explanations or operational analysis.

Nexus already has `policies`: deterministic CEL rules for request control. Those policies decide `allow`, `deny`, or `require_approval` for `requests`. That model must remain focused on approvals and execution control.

Product findings are different: they are observations generated from product
facts, such as an analysis result. They do not approve or deny an action.

## Decision

Nexus remains deterministic. Companion remains AI-assisted.

Products such as Argos own their business knowledge. Axis services store and execute product-owned configuration, but do not define vertical domain knowledge in code.

We add a Nexus `findings` module instead of overloading `requests/policies`.

- `policies` continue to be request control rules for `requests`.
- `finding_rules` are diagnostic rules over product facts.
- `fact_evaluations` persist submitted facts idempotently.
- `findings` persist deterministic results produced by matching rules.

We add a Companion `assist-packs` module instead of extending `nexus-assist`.

- `nexus-assist` stays focused on explaining Nexus requests and learning proposals.
- `assist_packs` store product-owned prompts/contracts.
- `assist_runs` persist AI outputs and status.
- Companion does not create or modify deterministic findings.

Current interpretation: Argos is an example product surface, not a platform
default. The same boundary applies to Ponti, Medmory, Pymes and future
products. Product-owned knowledge is loaded through contracts, rules, manifests
or assist packs; Axis services must not embed vertical business rules in shared
runtime code.

## Tenancy

`org_id` represents the customer organization.

`owner_system` represents who defines product knowledge. For Argos-owned rules
and packs, this is `argos`; for another product it is that product surface or
owner identifier.

`source_system` represents who produces facts.

`product_surface` represents where AI assistance is used.

Local development can expose product service principals with the narrow scopes
needed by the module being tested. For example:

- `ARGOS_ORG_ID=argos-local-org`
- Nexus exposes an `argos` service principal with `nexus:findings:read` and `nexus:findings:write`
- Companion exposes an `argos` service principal with `companion:assist:read` and `companion:assist:write`

## Minimal HTTP Contracts

Nexus findings:

- `POST /v1/finding-rules`
- `GET /v1/finding-rules`
- `GET /v1/finding-rules/archived`
- `GET /v1/finding-rules/{id}`
- `PATCH /v1/finding-rules/{id}`
- `POST /v1/finding-rules/{id}/archive`
- `POST /v1/finding-rules/{id}/restore`
- `DELETE /v1/finding-rules/{id}/hard`
- `POST /v1/fact-evaluations`
- `GET /v1/fact-evaluations/{id}`
- `GET /v1/findings`
- `GET /v1/findings/{id}`
- `PATCH /v1/findings/{id}`

Companion assist:

- `POST /v1/assist-packs`
- `GET /v1/assist-packs`
- `GET /v1/assist-packs/archived`
- `GET /v1/assist-packs/{id}`
- `PATCH /v1/assist-packs/{id}`
- `POST /v1/assist-packs/{id}/archive`
- `POST /v1/assist-packs/{id}/restore`
- `DELETE /v1/assist-packs/{id}/hard`
- `POST /v1/assist-runs`
- `GET /v1/assist-runs`
- `GET /v1/assist-runs/{id}`

## Consequences

Axis can support product-owned deterministic rules and product-owned AI prompts without hardcoding Argos knowledge in Nexus or Companion.

Argos can seed and evolve its own rules and assist packs. Nexus stores and evaluates the deterministic side. Companion stores and executes the AI side.

The platform cost is an extra boundary to maintain: request policies,
finding rules and assist packs are separate concepts. That separation is
intentional. Nexus policies must not become a generic facts engine for
approvals, and Companion assist must not become a deterministic decision engine.
