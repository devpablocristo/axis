# Autonomy And Capability Simplification

## Summary

Decision: simplify the model by making `Autonomy` the single scale.

The current v2 model has two parallel concepts:

```text
Virployee.autonomy = A0..A5
Capability.action_class = observe..write_high
```

They map 1:1 today, which makes the UI and domain language harder than it
needs to be. The cleaner model is:

```text
Virployee.autonomy >= Capability.required_autonomy
```

`Action Class` should disappear from the public language. A Capability should
declare the minimum autonomy required to assign it to a Virployee.

This document records the design decision implemented in v2.

## Current Problem

The existing pairing is technically valid but conceptually redundant:

| Current field | Owner | Meaning | Problem |
|---|---|---|---|
| `autonomy` | Virployee | How far the worker may go. | Correct core concept. |
| `action_class` | Capability | Minimum autonomy needed to use the capability. | Duplicates autonomy with different names. |

Current mapping:

| `action_class` | Required autonomy |
|---|---|
| `observe` | `A0` |
| `recommend` | `A1` |
| `draft` | `A2` |
| `write_low` | `A3` |
| `write_medium` | `A4` |
| `write_high` | `A5` |

Because this mapping is direct, `action_class` is not adding enough meaning to
justify being a public field.

## Proposed Model

Keep `Autonomy` as the only scale:

| Autonomy | Name | Meaning |
|---|---|---|
| `A0` | Conversation | Can converse and read contextual information, without recommending or preparing actions. |
| `A1` | Recommendation | Can read, analyze and recommend actions. |
| `A2` | Draft | Can prepare plans or executable drafts, without external side effects. |
| `A3` | Limited execution | Can execute low-risk writes that are reversible, idempotent and scoped to the tenant. |
| `A4` | Governed execution | Can attempt medium-risk actions only with prior approval or a controlled playbook. |
| `A5` | Broad autonomy | Reserved for broad multi-product autonomy; not enabled by default. |

Capability model should use:

```text
required_autonomy: A0 | A1 | A2 | A3 | A4 | A5
```

Assignment rule:

```text
Virployee.autonomy >= Capability.required_autonomy
```

Examples:

| User-facing capability | Required autonomy | Reason |
|---|---|---|
| `Leer eventos del calendario` | `A0` | Read/observe only. |
| `Redactar respuestas a mensajes` | `A2` | Prepares a draft, no external effect. |
| `calendar.events.create` | `A3` | Creates a low-risk, scoped record. |
| `billing.invoice.approve` | Later phase | Needs risk, policy and approval design before modeling. |

## Standards Comparison

| Reference | Useful idea | Design consequence for v2 |
|---|---|---|
| Google IAM | Permissions use `SERVICE.RESOURCE.VERB`; roles collect permissions. | Keep `capability_key = domain.resource.action` internally; show a clear ability phrase to people and do not overload either with autonomy. |
| Kubernetes RBAC | Rules separate resource and verb. | Keep the action in the key/action vocabulary, not in a second autonomy-like scale. |
| NIST ABAC | Decisions use subject, resource/object, action, environment and policy. | `required_autonomy` is only one attribute; policy/risk should stay separate later. |
| OAuth RAR | Rich authorization uses structured `type`, `actions`, resources and API-specific fields. | If capabilities become executable, add structured manifest fields later instead of stretching `required_autonomy`. |
| MCP Tools | Tools are callable functions with schemas. | Capability remains business ability; Tool/runtime stays separate. |
| v1 Companion | v1 manifests had `mode`, `risk_class`, `side_effect_type`, approval metadata and scopes. | Those belong to future execution/manifests, not to the minimal v2 Capability. |

## Decisions

- `Action Class` should disappear from Console and public API.
- `Capability.action_class` should be replaced by `Capability.required_autonomy`.
- The database should migrate from `action_class` to `required_autonomy`.
- New API payloads should accept and return `required_autonomy`.
- No public HTTP compatibility for `action_class` is required because v2 is still pre-release/internal.
- Existing v2 data should be preserved by mapping old values:

```text
observe      -> A0
recommend    -> A1
draft        -> A2
write_low    -> A3
write_medium -> A4
write_high   -> A5
```

## Implementation Shape

Backend:

- Capability domain uses `RequiredAutonomy`.
- Assignment validation uses direct
  `autonomy.Allows(requiredAutonomy)`.
- Create/update/list/get DTOs use `required_autonomy`.
- Clean and incremental migrations:
  - clean schema creates `required_autonomy`;
  - incremental migration adds `required_autonomy`, backfills from
    `action_class`, marks it not null, then drops `action_class`.

Console:

- Replace `Action Class` field with `Required autonomy`.
- Reuse the same autonomy labels and help bubble behavior used by Virployees.
- The help bubble should show only the selected autonomy value, not all values.

Docs:

- `capabilities.md` describes `required_autonomy`.
- `capabilities-review.md` supersedes the earlier `action_class`
  recommendation with this simplification.

## Test Plan

Companion:

- Create Capability with `required_autonomy=A2`.
- Reject invalid required autonomy.
- Virployee `A1` cannot receive an `A2` Capability.
- Virployee `A2` and above can receive an `A2` Capability.
- Existing data migration preserves old `action_class` semantics.

BFF:

- Gateway continues forwarding Capabilities unchanged except for the new JSON
  field.
- Tenant/membership validation remains unchanged.

Console:

- `Action Class` no longer appears.
- `Required autonomy` appears in create/edit/table.
- Bubble behavior matches Virployee Autonomy: same trigger, same volume of
  contextual information, selected value only.

Regression:

```bash
go test ./...        # v2/companion
go test ./...        # v2/bff
npm run typecheck    # v2/console
npm run build        # v2/console
```

## Explicit Non-Goals

- Do not add runtime execution.
- Do not add Tools.
- Do not add manifests.
- Do not add `mode`, `risk_class`, `side_effect_type`, scopes or approvals yet.
- Do not connect Nexus in this step.
- Keep `capability_key = domain.resource.action` as generated internal compatibility data; do not expose it as the user-facing Capability.

## Sources

- Google IAM roles and permissions:
  https://docs.cloud.google.com/iam/docs/roles-overview
- Kubernetes RBAC:
  https://kubernetes.io/docs/reference/access-authn-authz/rbac/
- NIST SP 800-162 ABAC:
  https://nvlpubs.nist.gov/nistpubs/specialpublications/nist.sp.800-162.pdf
- OAuth 2.0 Rich Authorization Requests, RFC 9396:
  https://datatracker.ietf.org/doc/html/rfc9396
- Model Context Protocol Tools:
  https://modelcontextprotocol.io/specification/draft/server/tools
