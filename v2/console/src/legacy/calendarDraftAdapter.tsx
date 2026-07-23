import type { VirployeeConfirmedDraft, VirployeeDryRun } from '../api'

export type LegacyCalendarDraftValues = {
  title: string
  date: string
  time: string
  timezone: string
  duration_minutes: string
  attendees: string
}

export type LegacyCalendarDraftKey = keyof LegacyCalendarDraftValues

export function isLegacyCalendarDraft(result: VirployeeDryRun): boolean {
  return result.intent.matched && result.draft.action === 'calendar.events.create'
}

export function legacyCalendarDraftValuesFromDryRun(
  result: VirployeeDryRun,
): LegacyCalendarDraftValues | null {
  if (!isLegacyCalendarDraft(result)) return null
  const values: LegacyCalendarDraftValues = {
    title: '',
    date: '',
    time: '',
    timezone: browserTimezone(),
    duration_minutes: '60',
    attendees: '',
  }
  for (const field of result.draft.fields) {
    if (field.key === 'title' || field.key === 'attendees') {
      values[field.key] = field.value
    }
    if (field.key === 'time') values.time = canonicalTime(field.value)
  }
  return values
}

export function isLegacyCalendarDraftComplete(values: LegacyCalendarDraftValues): boolean {
  return normalized(values.title).length > 0 &&
    /^\d{4}-\d{2}-\d{2}$/.test(normalized(values.date)) &&
    /^\d{2}:\d{2}$/.test(normalized(values.time)) &&
    normalized(values.timezone).length > 0 &&
    Number(values.duration_minutes) >= 5 &&
    Number(values.duration_minutes) <= 1440 &&
    normalized(values.attendees).length > 0
}

export function legacyCalendarConfirmedDraft(
  values: LegacyCalendarDraftValues,
): VirployeeConfirmedDraft {
  return {
    action: 'calendar.events.create',
    kind: 'calendar_event',
    fields: [
      { key: 'title', value: normalized(values.title) },
      { key: 'date', value: normalized(values.date) },
      { key: 'time', value: normalized(values.time) },
      { key: 'timezone', value: normalized(values.timezone) },
      { key: 'duration_minutes', value: normalized(values.duration_minutes) },
      { key: 'attendees', value: normalized(values.attendees) },
    ],
  }
}

export function LegacyCalendarDraftView(props: {
  draft: VirployeeDryRun['draft']
  values: LegacyCalendarDraftValues
  confirmed: boolean
  onValueChange: (key: LegacyCalendarDraftKey, value: string) => void
  onConfirm: () => void
}) {
  const complete = isLegacyCalendarDraftComplete(props.values)
  const reviewMessage = props.confirmed
    ? 'Draft confirmed'
    : complete
      ? 'Ready to check the gate.'
      : 'Complete the draft before checking the gate.'
  const clarifications = props.draft.missing_fields.filter((field) => {
    return isLegacyCalendarDraftKey(field.key) && normalized(props.values[field.key]).length === 0
  })

  return (
    <section className="virployee-preview__section" aria-label="Legacy calendar draft">
      <div className="virployee-section-heading">
        <span>V1 compatibility</span>
        <h3>Legacy draft</h3>
      </div>
      {clarifications.length > 0 ? (
        <div className="virployee-dry-run__clarifications" aria-label="Needs clarification">
          <strong>Needs clarification</strong>
          {clarifications.map((field) => (
            <span key={field.key}>{clarificationQuestion(field.key)}</span>
          ))}
        </div>
      ) : null}
      <div className="virployee-preview__grid">
        <label className="form-group">
          Action
          <input value="Create calendar event" readOnly />
        </label>
        <label className="form-group">
          Title
          <input
            value={props.values.title}
            onChange={(event) => props.onValueChange('title', event.currentTarget.value)}
            required
          />
          {normalized(props.values.title).length === 0 ? <span className="form-field-required">Required</span> : null}
        </label>
        <label className="form-group">
          Date
          <input
            type="date"
            value={props.values.date}
            onChange={(event) => props.onValueChange('date', event.currentTarget.value)}
            required
          />
          {normalized(props.values.date).length === 0 ? <span className="form-field-required">Required</span> : null}
        </label>
        <label className="form-group">
          Time
          <input
            type="time"
            value={props.values.time}
            onChange={(event) => props.onValueChange('time', event.currentTarget.value)}
            required
          />
          {normalized(props.values.time).length === 0 ? <span className="form-field-required">Required</span> : null}
        </label>
        <label className="form-group">
          Timezone
          <input
            value={props.values.timezone}
            onChange={(event) => props.onValueChange('timezone', event.currentTarget.value)}
            required
          />
          {normalized(props.values.timezone).length === 0 ? <span className="form-field-required">Required</span> : null}
        </label>
        <label className="form-group">
          Duration (minutes)
          <input
            type="number"
            min="5"
            max="1440"
            step="5"
            value={props.values.duration_minutes}
            onChange={(event) => props.onValueChange('duration_minutes', event.currentTarget.value)}
            required
          />
        </label>
        <label className="form-group full-width">
          Attendees
          <input
            value={props.values.attendees}
            onChange={(event) => props.onValueChange('attendees', event.currentTarget.value)}
            required
          />
          {normalized(props.values.attendees).length === 0 ? <span className="form-field-required">Required</span> : null}
        </label>
      </div>
      <div className="virployee-dry-run__draft-actions">
        <button
          type="button"
          className="btn-secondary"
          disabled={!complete || props.confirmed}
          onClick={props.onConfirm}
        >
          Confirm draft
        </button>
        <span className={complete || props.confirmed ? 'iam-control__inline-note' : 'iam-control__inline-error'}>
          {reviewMessage}
        </span>
      </div>
    </section>
  )
}

function isLegacyCalendarDraftKey(value: string): value is LegacyCalendarDraftKey {
  return value === 'title' ||
    value === 'date' ||
    value === 'time' ||
    value === 'timezone' ||
    value === 'duration_minutes' ||
    value === 'attendees'
}

function clarificationQuestion(value: string): string {
  if (value === 'title') return 'What is the event title?'
  if (value === 'date') return 'What date should I use?'
  if (value === 'time') return 'What time should I use?'
  if (value === 'timezone') return 'What timezone should I use?'
  if (value === 'duration_minutes') return 'How long should the event last?'
  if (value === 'attendees') return 'Who should be invited?'
  return 'Please complete the missing field.'
}

function browserTimezone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
}

function canonicalTime(value: string): string {
  const match = value.match(/(?:a las\s*)?(\d{1,2})(?::(\d{2}))?/i)
  if (!match) return ''
  const hour = Number(match[1])
  const minute = Number(match[2] ?? '0')
  if (hour > 23 || minute > 59) return ''
  return `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`
}

function normalized(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}
