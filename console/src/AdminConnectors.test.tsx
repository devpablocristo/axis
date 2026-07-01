import { render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { AdminConnectors } from './AdminConnectors'
import { createConnector, listConnectorTypes } from './api'

const crudPageProps = vi.hoisted(() => [] as Array<Record<string, unknown>>)

vi.mock('@devpablocristo/platform-crud-ui', () => ({
  crudStringsEs: {},
  CrudPage: (props: Record<string, unknown>) => {
    crudPageProps.push(props)
    const slot = props.listHeaderInlineSlot as (() => ReactNode) | undefined
    return (
      <div data-testid="crud-page">
        <h2>{String(props.labelPluralCap ?? '')}</h2>
        <p>{String(props.searchPlaceholder ?? '')}</p>
        <p>{String(props.emptyState ?? '')}</p>
        <p>{String(props.archivedEmptyState ?? '')}</p>
        <p>{String(props.trashEmptyState ?? '')}</p>
        {slot?.()}
      </div>
    )
  },
}))

vi.mock('./api', async () => {
  const actual = await vi.importActual<typeof import('./api')>('./api')
  return {
    ...actual,
    archiveConnector: vi.fn(),
    createConnector: vi.fn(),
    listConnectors: vi.fn(async () => []),
    listConnectorTypes: vi.fn(async () => [{
      kind: 'product-envelope-v1',
      name: 'Product envelope',
      description: 'Standard product connector',
      supports_test: true,
      supports_refresh: true,
      status: 'active',
      config_schema: {
        fields: [
          { key: 'base_url', label: 'Base URL', type: 'text', required: true },
          { key: 'secret_ref', label: 'Secret ref', type: 'text', required: true },
          { key: 'auth_type', label: 'Auth type', type: 'select', required: false, default_value: 'bearer', options: ['bearer'] },
          { key: 'timeout_ms', label: 'Timeout ms', type: 'number', required: false, default_value: 10000 },
        ],
      },
    }]),
    refreshConnector: vi.fn(),
    restoreConnector: vi.fn(),
    testConnector: vi.fn(),
    trashConnector: vi.fn(),
    updateConnector: vi.fn(),
  }
})

describe('AdminConnectors', () => {
  it('renders the shared CRUD surface from connector type schema', async () => {
    crudPageProps.length = 0

    render(<AdminConnectors orgId="org-a" tenantId="tenant-a" />)

    expect(await screen.findByText('Connectors')).toBeInTheDocument()
    await waitFor(() => {
      expect(listConnectorTypes).toHaveBeenCalledWith('org-a', 'tenant-a')
      expect(crudPageProps.at(-1)?.labelPluralCap).toBe('Connectors')
    })

    const props = crudPageProps.at(-1) as {
      formFields: Array<{ key: string; label: string }>
      supportsTrash?: boolean
      toolbarActions: Array<{ label: string }>
      emptyState?: string
      archivedEmptyState?: string
      trashEmptyState?: string
    }
    const labels = props.formFields.map((field) => field.label)
    expect(labels).toEqual(expect.arrayContaining([
      'Nombre',
      'Tipo',
      'Estado operativo',
      'Base URL',
      'Secret ref',
      'Auth type',
      'Timeout ms',
    ]))
    expect(labels.some((label) => label.includes('JSON'))).toBe(false)
    expect(props.supportsTrash).toBe(true)
    expect(props.toolbarActions.map((action) => action.label)).toEqual(['Activos', 'Archivados', 'Papelera'])
    expect(props.emptyState).toBe('Sin connectors')
    expect(props.archivedEmptyState).toBe('Sin connectors archivados')
    expect(props.trashEmptyState).toBe('Sin connectors en papelera')
  })

  it('creates connector config from human fields instead of JSON', async () => {
    crudPageProps.length = 0
    vi.mocked(createConnector).mockClear()

    render(<AdminConnectors orgId="org-a" tenantId="tenant-a" />)

    await waitFor(() => {
      expect(crudPageProps.at(-1)?.dataSource).toBeTruthy()
    })
    const props = crudPageProps.at(-1) as {
      dataSource: {
        create: (values: Record<string, string | boolean>) => Promise<void>
      }
    }

    await props.dataSource.create({
      name: 'Medmory',
      kind: 'product-envelope-v1',
      enabled: 'true',
      'config.base_url': 'https://medmory.local',
      'config.secret_ref': 'medmory-prod-token',
      'config.auth_type': '',
      'config.timeout_ms': '',
    })

    expect(createConnector).toHaveBeenCalledWith(
      'org-a',
      {
        name: 'Medmory',
        kind: 'product-envelope-v1',
        enabled: true,
        status: 'active',
        config: {
          base_url: 'https://medmory.local',
          secret_ref: 'medmory-prod-token',
          auth_type: 'bearer',
          timeout_ms: 10000,
        },
      },
      'tenant-a',
    )
  })
})
