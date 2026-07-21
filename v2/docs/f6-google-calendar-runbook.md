# F6 — Google Calendar real executor runbook

F6 lets a virployee create (and delete) **real** Google Calendar events when a
supervisor approves the action. The whole arc stays fail-closed: the event is only
written after a human approves, and every write is recorded in the run trace with
`mode=google_calendar` and `external_effects=true`.

This runbook covers the live smoke against a real calendar. The executor code and
its offline tests already ship on branch `fase6-executor-real`; what follows is the
provisioning + verification you run yourself (it needs Google credentials).

## Auth model

Service account + shared calendar. A Google service account writes to a calendar
that has been **shared with the service account's email**. Credentials come from
Application Default Credentials (ADC):

- **Local:** a service-account key JSON mounted into the companion container, with
  `GOOGLE_APPLICATION_CREDENTIALS` pointing at it.
- **Prod (Cloud Run):** attach the service account to the service (workload
  identity). Do **not** put the key JSON in an env var (ADR 0002).

No key material ever passes through Axis config, memory, or run traces.

## One-time Google setup

1. In a Google Cloud project, **enable the Google Calendar API**.
2. Create a **service account** and download a **JSON key** (local only).
3. Open the target Google Calendar → *Settings and sharing* → *Share with specific
   people* → add the service account email with **"Make changes to events"**.
4. Copy the calendar id (Settings → *Integrate calendar* → *Calendar ID*, e.g.
   `...@group.calendar.google.com`; a personal calendar id is your email).

## Local configuration

Put the key somewhere outside the repo and wire it through
`docker-compose.override.yml` (gitignored). Example:

```yaml
services:
  companion-v2:
    environment:
      COMPANION_V2_EXECUTION_MODE: google_calendar   # or "local,google_calendar"
      COMPANION_V2_GOOGLE_CALENDAR_ID: your-calendar-id@group.calendar.google.com
      GOOGLE_APPLICATION_CREDENTIALS: /secrets/gcal-sa.json
    volumes:
      - /abs/path/to/gcal-sa.json:/secrets/gcal-sa.json:ro
```

Then bring the stack up:

```bash
cd v2 && make up
```

If `COMPANION_V2_EXECUTION_MODE=google_calendar` is set without a calendar id,
companion refuses to start (fail-closed) — that is expected.

## Seed the demo data

```bash
cd v2 && make seed-demo
```

This provisions a demo virployee (autonomy A3) with the `calendar.events.create`
and `calendar.events.delete` capabilities. Both are write actions that require
approval; create's `rollback_capability_key` points at delete. (The seed talks to
the BFF, so it needs the BFF in dev-identity mode — `BFF_V2_IDENTITY_PROVIDER=dev`.)

## Smoke — create

1. Open the console (`http://localhost:19173`), find the demo virployee.
2. Dry-run / execution-gate a request like
   *"Agendá una reunión mañana a las 15 con ana@example.com"* with the draft
   confirmed → the gate returns **require_approval** (a write with external
   effects is never auto-allowed).
3. Go to **Approvals**, approve it.
4. Execute the approved action.

Verify:

- The event appears in the **real** Google Calendar.
- The run trace shows `mode=google_calendar`, `external_effects=true`, and a
  `resource_id` (the Google event id).
- Re-running the execution does **not** create a second event (the idempotency key
  is the event id → a duplicate insert is a safe replay).

## Smoke — rollback (compensation)

Delete is a governed compensation with its **own** binding — the create's approval
can never authorize it.

1. Request a delete for that event (`calendar.events.delete`), supplying the event
   id as the `event_reference`.
2. Approve it, execute it.
3. The event disappears from the calendar. Deleting an already-gone event succeeds
   (idempotent).

## Troubleshooting

- **companion won't start:** `google_calendar` mode is set but
  `COMPANION_V2_GOOGLE_CALENDAR_ID` is empty, or ADC could not resolve credentials.
- **403 "caller does not have permission":** the calendar is not shared with the
  service account email (or not with edit rights).
- **seed returns 401:** the BFF is not in dev-identity mode
  (`BFF_V2_IDENTITY_PROVIDER=dev`); the seed authenticates with dev headers.
- **execution says "executor is not configured":** the capability
  (`calendar.events.create`/`delete`) is not assigned to the virployee, or the
  mode is not enabled.
