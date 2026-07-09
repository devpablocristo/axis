# Approval flow checkpoint

This checkpoint freezes new execution features and verifies the minimum Companion -> Nexus -> Approval -> Trace loop.

## Flow

1. Companion receives a virployee execution-gate request.
2. Companion builds a dry-run result and only calls Nexus if the local gate passes.
3. Nexus evaluates the action type for the same tenant.
4. Nexus returns one of:
   - `allow`: action may move forward in simulation mode; no approval is created.
   - `deny`: action is blocked; no approval is created.
   - `require_approval`: action is blocked and Nexus creates a pending approval.
5. Companion records a run trace with the Nexus decision, reason, risk, binding hash, and approval metadata when present.
6. Console shows the run trace in the virployee history.
7. Console shows approvals in pending, approved, and rejected views.
8. A human can approve or reject a pending approval.

## Current guarantees

- `binding_hash` ties the governance decision to the evaluated input.
- Approvals are durable in Nexus.
- Run traces are durable in Companion.
- `allow`, `deny`, `require_approval`, and Nexus unavailable are visible in traces.
- Approval state can be read after the original trace was created.

## Deliberately out of scope

- No automatic execution after approval.
- No policy engine, CEL, callbacks, break-glass, or audit chain.
- No external calendar execution.
- No retry/resume worker.

## Acceptance smoke

Run the stack and then:

```bash
cd v2
bash scripts/smoke-approval-flow.sh
```

The smoke creates or reuses demo data for the dev tenant, checks `allow`, creates and approves a `require_approval`, verifies the virployee run history, then disables the create action type to verify `deny`.
