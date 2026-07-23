# Product integration contract

Axis has one ownership relation:

```text
organization → products
```

Every product belongs to exactly one organization. An integration installs
behavior for that existing product; it does not create a tenant, duplicate the
organization-product relation, or add product-specific code to Axis.

## Functional v3 contract

New installations publish `axis.product-integration.v3`. Its public shape is
service-neutral:

- `entrypoints` name Virployee or routing-pool UUIDs;
- `capabilities` use canonical UUIDs and bind a manifest, executor operation
  and input/output schema hashes;
- `events` carry a versioned JSON Schema and its SHA-256;
- `governed_operations` bind capability UUID, operation and required scopes;
- `connector_bindings` name an executor binding, operation and optional
  `secret_ref`; and
- `authentication` and `limits` bound the machine credential.

The contract contains no `required_services`, `services`, BFF, Companion,
Nexus or internal URL. The normative schema is
[`product-integration.v3.schema.json`](../contracts/schemas/product-integration.v3.schema.json).

`axis.product-integration.v2` remains immutable. A compatibility translator
projects an active v2 contract into the functional representation. New
installations use v3, and v2 support is removed only after there are no active
v2 versions.

## Ownership and activation

BFF owns the immutable contract versions, the active-version pointer and
persistent machine credentials. An `IntegrationParticipant` registry is
configured in the BFF composition root. Each participant projects only the
functional subset it owns and returns its own immutable validation/readiness
snapshot; the application use case does not switch on a service name.

Validation produces an immutable report bound to the contract and participant
snapshot hashes. Activation:

1. validates a report for the exact version from the last 24 hours;
2. activates each applicable participant idempotently; and
3. moves the BFF active pointer only after all applicable participants agree.

There is no distributed transaction, task plan or compensation. A failed
activation leaves the previous global version active. Suspending in BFF cuts
off external ingress immediately. An unavailable participant does not prevent
BFF from starting, but every operation that requires it fails closed and its
readiness is reported as unavailable.

## Trusted machine context

API keys are generated once, stored hashed, rotatable and revocable. A
credential grants a technical principal and contract-declared scopes. It never
selects an organization or embeds a Virployee as implicit authority.

BFF strips caller authority headers and forwards trusted context:

```text
X-Org-ID
X-Product-ID
X-Axis-Product-ID
X-Product-Surface
X-Axis-Integration-ID
X-Axis-Integration-Version
X-Axis-Integration-Hash
X-Axis-Principal-Type
X-Axis-Principal-ID
X-Axis-Principal-Scopes
X-Axis-Access-Mode
```

This is the wire projection of `axis.invocation-context.v1`. Companion persists
the complete context with the assist run, and the governance adapter propagates
it as `via_orchestrator`. Missing, suspended, incompatible or hash-drifted
installations fail closed.

Persisted credentials are authoritative. The environment binding parser is
available only in development/test with an explicit
`BFF_V2_ALLOW_LEGACY_PRODUCT_API_KEYS=true`. It is never used for a persisted
key prefix, after revocation, or after a credential-repository error.

## Management API

The browser uses BFF only:

```text
GET|POST /api/organizations/:org/products/:product/integration/versions
POST     /api/organizations/:org/products/:product/integration/versions/:id/validate
POST     /api/organizations/:org/products/:product/integration/versions/:id/activate
POST     /api/organizations/:org/products/:product/integration/suspend|retire
GET      /api/organizations/:org/products/:product/integration/readiness
GET|POST /api/organizations/:org/products/:product/integration/credentials
POST     /api/organizations/:org/products/:product/integration/credentials/:id/rotate|revoke
```

Products use the authenticated BFF facade:

```text
POST /v1/assist-runs
GET  /v1/assist-runs/:run_id
GET  /v1/assist-capabilities
POST /v1/product-events
```

Starting an assist requires `assist.write`. BFF keeps the public event field
`version`, translates it to the internal compatibility DTO, and temporarily
propagates both product-ID headers. Events require a UUID event ID and preserve
one idempotency binding through detection and any governed action. The facade
contract is
[`bff-facade.v1.yaml`](../contracts/openapi/bff-facade.v1.yaml).

## Connectors

An organization-specific executor implements `axis.connector.v1`. Descriptors
bind an operation to a capability UUID and schemas. Invocation is bounded,
idempotent and HMAC-authenticated through a `secret_ref`; result lookup resolves
ambiguous timeouts. HTTPS is mandatory outside development. The normative
contracts are
[`connector.v1.schema.json`](../contracts/schemas/connector.v1.schema.json) and
[`connector.v1.yaml`](../contracts/openapi/connector.v1.yaml).

Capability names are for humans. The UUID is canonical. A legacy
`capability_key` may be accepted as an input alias during backfill, but it never
selects executable code.

## Anti-coupling rule

Axis contains no branch, alias, fixture, credential, default or schema named
after a consuming product. A consumer may configure Axis using this contract;
it may not modify Axis internals or become a dependency of BFF, Companion or
Nexus. See [Axis v2 hexagonal boundaries](hexagonal-boundaries.md).
