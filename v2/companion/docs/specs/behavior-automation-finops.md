# Prompt governance, business watchers, evaluations and FinOps

## Prompt bundles

Companion owns immutable prompt assets and versions. Resolution order is Axis
safety base, Job Role, Profile Template and Virployee. A product binding wins
over an organization default at the same level. Arbitrary interpolation of
payloads, memories, documents or conversations is rejected during simulation.

Promotion needs a passing synthetic evaluation for the exact prompt content,
product and snapshot hash, completed less than 24 hours earlier. The approver
must differ from the version creator and evaluation runner. Nexus supplies a
metadata-only authorization hash; it never receives prompt content. Assist
persists the effective versions and `prompt_bundle_hash`, which participates in
its context binding.

Existing profile-template prompts are backfilled as
`evaluation_unknown`. They continue to run, while all new promotions use the
governed path.

## Behavior evaluations

Suite versions contain synthetic fixtures and thresholds. Runs use no-effect
executors and bind prompt/capability/Virployee snapshot hashes. Reports are
immutable. Isolation, leakage and approval-bypass cases have zero tolerance.
Changing any bound artifact makes the report stale.

## Business watchers

Business watchers are separate from operational reconciliation watchers. Each
immutable version has one `schedule` or `event` trigger, an exact
active/conformant read capability detector, and at most one action capability.
Occurrences are deduplicated per watcher and stable occurrence key.

Modes:

- `observe`: record the occurrence only.
- `propose`: persist a typed proposal; this is the default.
- `execute_if_authorized`: call the shared ToolInvocationGate.

Every action is re-evaluated against assignment, subject/case, delegation,
autonomy, professional policy, quotas and Nexus. Write retries preserve the
same idempotency key. Watchers do not implement arbitrary code, SQL, task plans
or compensations.

## FinOps

The FinOps ledger is append-only and separate from quota reservations and usage.
It attributes actual runtime consumption to organization, stable product,
service/area, Virployee, capability/version and model. Unknown pricing is
`unpriced`, never zero. Corrections append adjustment events.

Budgets cover an organization or product for a UTC calendar month and default
to informational 80% and 100% thresholds. They may create Nexus incidents but
never block execution; quota policies alone control consumption.
