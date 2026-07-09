import type { CrudFormValues, CrudPageProps } from '@devpablocristo/platform-crud-ui'
import type { FormEvent } from 'react'

type EntityFormPanelProps<T extends { id: string }> = {
  title: string
  mode: 'create' | 'edit'
  fields: NonNullable<CrudPageProps<T>['formFields']>
  values: CrudFormValues
  saving: boolean
  primaryLabel: string
  valid: boolean
  onChange: (values: CrudFormValues) => void
  onSubmit: () => void
  onCancel: () => void
}

export function EntityFormPanel<T extends { id: string }>(props: EntityFormPanelProps<T>) {
  const activeFields = props.fields.filter((field) => {
    if (props.mode === 'edit' && field.createOnly) return false
    if (props.mode === 'create' && field.editOnly) return false
    return true
  })

  const submit = (event: FormEvent) => {
    event.preventDefault()
    props.onSubmit()
  }

  return (
    <div className="card crud-form-card axis-entity-form-card">
      <div className="card-header">
        <h2>{props.title}</h2>
      </div>
      <form className="crud-form axis-entity-form" onSubmit={submit}>
        <div className="axis-entity-form-actions axis-entity-form-actions--top">
          <button type="submit" className="btn-primary" disabled={props.saving || !props.valid}>
            {props.saving ? 'Saving...' : props.primaryLabel}
          </button>
          <button type="button" className="btn-secondary" disabled={props.saving} onClick={props.onCancel}>
            Cancel
          </button>
        </div>
        <div className="crud-form-grid">
          {activeFields.map((field) => (
            <div key={field.key} className={`form-group${field.fullWidth ? ' full-width' : ''}`}>
              <label htmlFor={`crud-field-${field.key}`}>
                {field.label}
                {field.required ? ' *' : ''}
              </label>
              {field.type === 'textarea' ? (
                <textarea
                  id={`crud-field-${field.key}`}
                  rows={field.rows ?? 3}
                  value={String(props.values[field.key] ?? '')}
                  onChange={(event) => props.onChange({ ...props.values, [field.key]: event.target.value })}
                  placeholder={field.placeholder}
                />
              ) : field.type === 'select' ? (
                <select
                  id={`crud-field-${field.key}`}
                  value={String(props.values[field.key] ?? '')}
                  onChange={(event) => props.onChange({ ...props.values, [field.key]: event.target.value })}
                >
                  <option value="">{field.placeholder ?? 'Select an option'}</option>
                  {(field.options ?? []).map((option) => (
                    <option key={option.value} value={option.value}>{option.label}</option>
                  ))}
                </select>
              ) : field.type === 'checkbox' ? (
                <label className="toggle">
                  <input
                    id={`crud-field-${field.key}`}
                    aria-label={field.label}
                    type="checkbox"
                    checked={Boolean(props.values[field.key])}
                    onChange={(event) => props.onChange({ ...props.values, [field.key]: event.target.checked })}
                  />
                  <span className="toggle-track" />
                  <span className="toggle-thumb" />
                </label>
              ) : (
                <input
                  id={`crud-field-${field.key}`}
                  type={field.type ?? 'text'}
                  value={String(props.values[field.key] ?? '')}
                  onChange={(event) => props.onChange({ ...props.values, [field.key]: event.target.value })}
                  placeholder={field.placeholder}
                />
              )}
            </div>
          ))}
        </div>
        <footer className="axis-entity-form-actions axis-entity-form-actions--bottom">
          <button type="submit" className="btn-primary" disabled={props.saving || !props.valid}>
            {props.saving ? 'Saving...' : props.primaryLabel}
          </button>
          <button type="button" className="btn-secondary" disabled={props.saving} onClick={props.onCancel}>
            Cancel
          </button>
        </footer>
      </form>
    </div>
  )
}

export function emptyFormValues<T extends { id: string }>(
  fields: NonNullable<CrudPageProps<T>['formFields']>,
): CrudFormValues {
  return Object.fromEntries(
    fields
      .filter((field) => !field.editOnly)
      .map((field) => [field.key, field.type === 'checkbox' ? false : '']),
  ) as CrudFormValues
}
