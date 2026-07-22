# UI QA checklist

Use this checklist after UI changes and before adding new product features.

## Setup

```bash
cd v2
make up
make seed-demo
```

Open `http://localhost:19173`.

## Navigation and state

- Sidebar groups are ordered as Operate, Builder, Admin.
- Org/Product selectors load the expected organization.
- Refresh session does not clear the selected page unexpectedly.
- Empty states and errors use the same visual treatment across sections.

## CRUD surfaces

- Virployees, Capabilities, Job Roles, Profile Templates, Users, Organizations, Orgs, and Products use one top action bar.
- Buttons use consistent colors, spacing, casing, disabled states, and danger styling.
- Selection checkboxes and primary name columns stay visible while horizontally scrolling.
- Created timestamps are visible and sortable where list tables support sorting.

## Approval flow

- A selected virployee opens Preview and Dry Run without changing list layout.
- Dry Run can run a read action and show an allowed result.
- A calendar create draft can be completed and checked through the execution gate.
- `require_approval` shows a pending approval checkpoint and a Review approval action.
- Approving or rejecting removes decision buttons from the approval card.
- Returning to the Virployee keeps the Dry Run panel open and refreshes the run history.
- Approved/rejected approval traces do not show as blocked.
- Approved prepared actions show `Execute locally` in the Virployee Dry Run panel.
- Local execution records a run history row with resource and Nexus report status.

## Automated checks

```bash
make test-e2e
docker compose exec -T console-v2 sh -lc 'npm run typecheck && npm run build'
```
