import { KeyRound, Link2, RefreshCw, ShieldCheck } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import {
  activateProductIntegrationVersion,
  createProductIntegrationCredential,
  createProductIntegrationVersion,
  getProductIntegration,
  getProductIntegrationReadiness,
  listProductIntegrationCredentials,
  validateProductIntegrationVersion,
  type ProductIntegration,
  type ProductIntegrationContract,
  type ProductIntegrationCredential,
  type ProductIntegrationReadiness,
  type ProductIntegrationV3Contract,
  type ProductIntegrationVersion,
} from './api'
import { formatDateTime24 } from './formatters'

type Props = {
  organizationId: string
  productId: string
  productSurface: string
  principalId: string
}

const defaultContract: ProductIntegrationV3Contract = {
  schema_version: 'axis.product-integration.v3',
  entrypoints: [],
  capabilities: [],
  events: [],
  governed_operations: [],
  connector_bindings: [],
  authentication: {
    mode: 'api_key',
    scopes: ['assist.write'],
  },
  limits: {
    max_request_bytes: 1048576,
    max_result_bytes: 1048576,
    rate_per_minute: 60,
  },
}

export function ProductIntegrationPage({
  organizationId,
  productId,
  productSurface,
  principalId,
}: Props) {
  const [integration, setIntegration] = useState<ProductIntegration | null>(null)
  const [versions, setVersions] = useState<ProductIntegrationVersion[]>([])
  const [readiness, setReadiness] = useState<ProductIntegrationReadiness | null>(null)
  const [credentials, setCredentials] = useState<ProductIntegrationCredential[]>([])
  const [contract, setContract] = useState(formatContract(defaultContract))
  const [draftSource, setDraftSource] = useState('New v3 contract')
  const [latestSecret, setLatestSecret] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const participants = useMemo(
    () => readiness?.participants ?? readiness?.services ?? {},
    [readiness],
  )
  const credentialScopes = useMemo(() => {
    const activeVersion = versions.find((version) => version.id === integration?.active_version_id)
    return integrationScopes(activeVersion?.contract)
  }, [integration?.active_version_id, versions])
  const schemaVersion = useMemo(() => contractSchemaVersion(contract), [contract])

  const load = useCallback(async () => {
    if (!organizationId || !productId) return
    setError('')
    try {
      const [state, ready, keys] = await Promise.all([
        getProductIntegration(organizationId, productId, principalId),
        getProductIntegrationReadiness(organizationId, productId, principalId),
        listProductIntegrationCredentials(organizationId, productId, principalId),
      ])
      setIntegration(state.integration)
      setVersions(state.versions ?? [])
      setReadiness(ready)
      setCredentials(keys)
    } catch (cause) {
      setError(message(cause, 'Could not load product integration'))
    }
  }, [organizationId, principalId, productId])

  useEffect(() => {
    void load()
  }, [load])

  const mutate = async (action: () => Promise<void>) => {
    if (busy) return
    setBusy(true)
    setError('')
    try {
      await action()
      await load()
    } catch (cause) {
      setError(message(cause, 'Integration change failed'))
    } finally {
      setBusy(false)
    }
  }

  if (!productId) {
    return <section className="empty-state">Select a product that belongs to this organization.</section>
  }

  return (
    <section className="product-integration">
      <header className="domain-banner domain-banner--administration">
        <div>
          <Link2 aria-hidden="true" />
          <span>
            <strong>{productSurface} integration contract</strong>
            <small>Versioned entrypoints, capabilities, events and governed operations.</small>
          </span>
        </div>
        <button type="button" className="btn-secondary" onClick={() => void load()}>
          <RefreshCw aria-hidden="true" />
          Refresh
        </button>
      </header>

      {error ? <p role="alert" className="iam-control__inline-error">{error}</p> : null}

      <div className="integration-status-strip">
        <Status value={readiness?.status || 'unknown'} />
        <span>Lifecycle <strong>{integration?.lifecycle || readiness?.lifecycle || 'not installed'}</strong></span>
        <span>Active revision <strong>{readiness?.version ?? '—'}</strong></span>
        <code>{short(readiness?.contract_hash || '')}</code>
      </div>

      <div className="operations-grid">
        <article className="card operations-card">
          <Heading
            icon={<ShieldCheck aria-hidden="true" />}
            title="Draft a contract"
            note="Activation succeeds after every registered participant validates the exact contract hash."
          />
          <form
            className="operations-form"
            onSubmit={(event) => {
              event.preventDefault()
              void mutate(async () => {
                const parsed = parseContract(contract)
                await createProductIntegrationVersion(organizationId, productId, parsed, principalId)
              })
            }}
          >
            <div className="integration-contract-meta">
              <Status value={
                schemaVersion === 'axis.product-integration.v3'
                  ? 'v3'
                  : schemaVersion === 'invalid'
                    ? 'invalid'
                    : 'compatibility'
              } />
              <span>{draftSource}</span>
            </div>
            <label className="form-group">
              Contract JSON
              <textarea
                rows={18}
                spellCheck={false}
                value={contract}
                onChange={(event) => {
                  setContract(event.currentTarget.value)
                  setDraftSource('Edited draft')
                }}
              />
            </label>
            <button type="submit" className="btn-primary" disabled={busy}>
              Create immutable version
            </button>
          </form>
        </article>

        <article className="card operations-card operations-card--wide">
          <Heading
            title="Version history"
            note="V2 contracts remain visible and can be used as compatibility drafts while new installations use v3."
          />
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Revision</th>
                  <th>Contract</th>
                  <th>State</th>
                  <th>Hash</th>
                  <th>Created</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {versions.length ? versions.map((item) => (
                  <tr key={item.id}>
                    <td>v{item.revision}</td>
                    <td><code>{versionSchema(item)}</code></td>
                    <td><Status value={item.status} /></td>
                    <td><code>{short(item.contract_hash)}</code></td>
                    <td>{formatDateTime24(item.created_at)}</td>
                    <td className="operations-row-actions">
                      {item.contract ? (
                        <button
                          type="button"
                          className="btn-secondary"
                          disabled={busy}
                          onClick={() => {
                            setContract(formatContract(item.contract as ProductIntegrationContract))
                            setDraftSource(`Revision v${item.revision} · ${versionSchema(item)}`)
                          }}
                        >
                          Use as draft
                        </button>
                      ) : null}
                      <button
                        type="button"
                        className="btn-secondary"
                        disabled={busy}
                        onClick={() => void mutate(async () => {
                          await validateProductIntegrationVersion(organizationId, productId, item.id, principalId)
                        })}
                      >
                        Validate
                      </button>
                      <button
                        type="button"
                        className="btn-primary"
                        disabled={busy || item.status === 'active'}
                        onClick={() => void mutate(async () => {
                          await activateProductIntegrationVersion(organizationId, productId, item.id, principalId)
                        })}
                      >
                        Activate
                      </button>
                    </td>
                  </tr>
                )) : (
                  <tr>
                    <td colSpan={6} className="operations-empty">No integration version exists.</td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </article>

        <article className="card operations-card">
          <Heading
            icon={<KeyRound aria-hidden="true" />}
            title="Machine credential"
            note="The secret appears once and is limited to the scopes declared by this integration."
          />
          <button
            type="button"
            className="btn-primary"
            disabled={busy || !integration}
            onClick={() => void mutate(async () => {
              const credential = await createProductIntegrationCredential(
                organizationId,
                productId,
                {
                  service_principal: `integration:${productSurface}`,
                  scopes: credentialScopes,
                },
                principalId,
              )
              setLatestSecret(credential.secret || '')
            })}
          >
            Create API key
          </button>
          {latestSecret ? (
            <div className="integration-secret">
              <strong>Copy now</strong>
              <code>{latestSecret}</code>
            </div>
          ) : null}
          <div className="operations-mini-list">
            {credentials.map((item) => (
              <div key={item.id}>
                <span>
                  <strong>{item.service_principal}</strong>
                  <small>{item.key_prefix} · {item.scopes.join(', ')}</small>
                </span>
                <Status value={item.status} />
              </div>
            ))}
          </div>
        </article>

        <article className="card operations-card">
          <Heading
            title="Participant readiness"
            note="Participants are discovered from the active contract; unavailable is never displayed as healthy."
          />
          <div className="operations-mini-list">
            {Object.entries(participants).map(([name, value]) => (
              <div key={name}>
                <strong>{name}</strong>
                <Status value={value.status} />
              </div>
            ))}
            {Object.keys(participants).length === 0 ? (
              <p className="axis-muted">No participant readiness reported.</p>
            ) : null}
          </div>
        </article>
      </div>
    </section>
  )
}

function Heading({ icon, title, note }: { icon?: ReactNode; title: string; note?: string }) {
  return (
    <div className="card-header operations-card__heading">
      <div>
        {icon}
        <span>
          <h3>{title}</h3>
          {note ? <p>{note}</p> : null}
        </span>
      </div>
    </div>
  )
}

function Status({ value }: { value: string }) {
  const tone = ['active', 'ready', 'serving', 'validated', 'v3'].includes(value)
    ? 'success'
    : ['blocked', 'failed', 'suspended', 'retired', 'invalid'].includes(value)
      ? 'danger'
      : 'warning'
  return <span className={`axis-status-badge axis-status-badge--${tone}`}>{value}</span>
}

function versionSchema(version: ProductIntegrationVersion): string {
  return version.schema_version || version.contract?.schema_version || 'axis.product-integration.v2'
}

function contractSchemaVersion(value: string): string {
  try {
    const parsed = JSON.parse(value) as { schema_version?: unknown }
    return typeof parsed.schema_version === 'string' ? parsed.schema_version : 'unknown'
  } catch {
    return 'invalid'
  }
}

function parseContract(value: string): ProductIntegrationContract {
  const parsed: unknown = JSON.parse(value)
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('Contract JSON must be an object.')
  }
  const schemaVersion = (parsed as { schema_version?: unknown }).schema_version
  if (typeof schemaVersion !== 'string' || schemaVersion.trim().length === 0) {
    throw new Error('Contract JSON requires schema_version.')
  }
  return parsed as ProductIntegrationContract
}

function integrationScopes(contract: ProductIntegrationContract | undefined): string[] {
  if (!contract) return ['assist.write']
  const authentication = contract.authentication
  if (!authentication || typeof authentication !== 'object' || Array.isArray(authentication)) {
    return ['assist.write']
  }
  const scopes = (authentication as { scopes?: unknown }).scopes
  if (!Array.isArray(scopes)) return ['assist.write']
  const values = scopes.filter((scope): scope is string => typeof scope === 'string' && scope.trim().length > 0)
  return values.length > 0 ? values : ['assist.write']
}

function formatContract(value: ProductIntegrationContract): string {
  return JSON.stringify(value, null, 2)
}

function short(value: string) {
  if (!value) return '—'
  return value.length > 18 ? `${value.slice(0, 10)}…${value.slice(-6)}` : value
}

function message(cause: unknown, fallback: string) {
  return cause instanceof Error ? cause.message : fallback
}
