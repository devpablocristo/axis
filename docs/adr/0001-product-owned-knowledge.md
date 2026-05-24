# ADR 0001: Product-Owned Knowledge Across Nexus and Companion

Status: accepted

## Context

Axis hosts shared services used by multiple products. Argos needs to store and run rules over image-analysis facts, and also request AI assistance for user-facing explanations.

Nexus already has `policies`: deterministic CEL rules for request control. Those policies decide `allow`, `deny`, or `require_approval` for `requests`. That model must remain focused on approvals and execution governance.

Argos findings are different: they are observations generated from product facts, such as an analysis result. They do not approve or deny an action.

## Decision

Nexus remains deterministic. Companion remains AI-assisted.

Products such as Argos own their business knowledge. Axis services store and execute product-owned configuration, but do not define vertical domain knowledge in code.

We add a Nexus `findings` module instead of overloading `requests/policies`.

- `policies` continue to be governance rules for `requests`.
- `finding_rules` are diagnostic rules over product facts.
- `fact_evaluations` persist submitted facts idempotently.
- `findings` persist deterministic results produced by matching rules.

We add a Companion `assist-packs` module instead of extending `nexus-assist`.

- `nexus-assist` stays focused on explaining Nexus requests and learning proposals.
- `assist_packs` store product-owned prompts/contracts.
- `assist_runs` persist AI outputs and status.
- Companion does not create or modify deterministic findings.

## Tenancy

`org_id` represents the customer organization.

`owner_system` represents who defines product knowledge. For Argos-owned rules and packs, this is `argos`.

`source_system` represents who produces facts. For Argos analysis facts, this is `argos`.

`product_surface` represents where AI assistance is used. For Argos UI flows, this is `argos`.

For local development:

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
