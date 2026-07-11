# Approval flow checkpoint

This checkpoint verifies the minimum Companion -> Nexus -> Approval -> Trace loop and the first safe post-approval continuation.

## Quick start

```bash
cd v2
make up
```

Open `http://localhost:19173`.

To create a reusable demo virployee and a pending approval:

```bash
make seed-demo
```

The seed is additive. It reuses the base action types/capabilities and updates the demo virployee, but it does not delete existing data and does not approve or reject the created approval.

## Current flow

1. Companion receives a virployee dry-run or execution-gate request.
2. Companion builds the runtime context and evaluates capability/autonomy locally.
3. Companion calls Nexus only when the local execution gate passes.
4. Nexus returns `allow`, `deny`, or `require_approval`.
5. For `require_approval`, Nexus creates a durable pending approval.
6. Companion records a durable run trace with Nexus decision, risk, reason, binding hash, and approval metadata.
7. Console shows the run trace in the Virployee dry-run history.
8. Console lets a human approve/reject from Approvals and return to the Virployee.
9. The human confirms an exact date, 24-hour time, IANA timezone, duration and attendees.
10. If approved, a human can manually trigger the local execution from the Virployee Dry Run panel.
11. Companion validates the approval binding, creates the event idempotently and reports the result to Nexus.

## Automated checks

Run all e2e checks in Docker:

```bash
cd v2
make test-e2e
```

Individual checks:

```bash
make test-console-e2e
make test-approval-flow-e2e
make test-console-real-e2e
```

`test-approval-flow-e2e` approves one request, executes it locally, and verifies `allow`, `require_approval`, `deny`, idempotent execution and Nexus result reporting.
`test-console-real-e2e` creates real data through BFF, drives the UI, approves a pending approval, executes it locally, and checks that the Virployee history reflects both `Approved` and `Executed`.

## Manual UI check

1. Run `make seed-demo`.
2. Open `http://localhost:19173`.
3. Go to `Virployees`.
4. Search `Demo Approval Virployee`.
5. Select it and open `Dry Run`.
6. Confirm the history includes an allowed read and a create request requiring approval.
7. Go to `Approvals`.
8. Open the pending approval created by the seed.
9. Approve or reject it.
10. Return to the Virployee and confirm the history shows the human decision.
11. Click `Execute locally` and confirm the exact prepared action.
12. Confirm the history shows `Execution`, `Executed`, a resource ID and a reported Nexus result.

## Current guarantees

- `binding_hash` ties the governance decision to the evaluated input.
- Approvals are durable in Nexus.
- Run traces are durable in Companion.
- `allow`, `deny`, `require_approval`, and Nexus unavailable are visible in traces.
- Approval state can be read after the original trace was created.
- Approved approvals can be executed manually only when the durable prepared action and `binding_hash` still match.
- Repeating the same execution returns the existing trace and local event.
- Nexus stores the idempotent execution result tied to the governance check.

## Deliberately out of scope

- No policy engine, CEL, callbacks, break-glass, or audit chain.
- No external calendar provider or OAuth integration.
- No retry/resume worker.
- No automatic execution after approval.
