import { render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { AdminControlCenter } from './AdminControlCenter'
import { listTools } from './api'

const crudPageProps = vi.hoisted(() => [] as Array<Record<string, unknown>>)

vi.mock('@devpablocristo/platform-crud-ui', () => ({
  crudStringsEs: {},
  CrudPage: (props: Record<string, unknown>) => {
    crudPageProps.push(props)
    const columns = (props.columns ?? []) as Array<{ header?: string }>
    return (
      <div data-testid="crud-page">
        <h2>{String(props.labelPluralCap ?? '')}</h2>
        <p>{String(props.searchPlaceholder ?? '')}</p>
        <div>
          {columns.map((column, index) => <span key={`${column.header ?? 'column'}-${index}`}>{column.header ?? ''}</span>)}
        </div>
      </div>
    )
  },
}))

vi.mock('./api', async () => {
  const actual = await vi.importActual<typeof import('./api')>('./api')
  return {
    ...actual,
    listTools: vi.fn(async () => [
      {
        tool_id: '11111111-1111-4111-8111-111111111111',
        tool_key: 'medical.patient.read',
        name: 'Leer historia clínica',
        description: 'Obtiene datos clínicos del paciente',
        operation: 'read',
        side_effect: false,
        status: 'active',
        capability_key: 'medical.case.review',
      },
      {
        tool_id: '22222222-2222-4222-8222-222222222222',
        tool_key: 'billing.invoice.pay',
        name: 'Pagar factura',
        operation: 'pay',
        side_effect: true,
        status: 'disabled',
      },
    ]),
  }
})

describe('AdminControlCenter', () => {
  it('uses the shared CrudPage shell for the Tools catalog', async () => {
    crudPageProps.length = 0

    render(<AdminControlCenter orgId="org-a" tenantId="tenant-a" />)

    expect(screen.getByTestId('crud-page')).toBeInTheDocument()
    expect(screen.getAllByText('Tools').length).toBeGreaterThan(0)
    expect(screen.getByText('Buscar tools')).toBeInTheDocument()
    expect(screen.getByText('Tool')).toBeInTheDocument()
    expect(screen.getByText('Key')).toBeInTheDocument()
    expect(screen.getByText('Operación')).toBeInTheDocument()
    expect(screen.getByText('Side effect')).toBeInTheDocument()
    expect(screen.getByText('Estado')).toBeInTheDocument()
    expect(screen.getByText('Capability')).toBeInTheDocument()

    const props = crudPageProps.at(-1) as {
      allowCreate?: boolean
      allowEdit?: boolean
      allowArchive?: boolean
      allowTrash?: boolean
      toolbarActions: Array<{ label: string; onClick: () => void }>
      dataSource: { list: () => Promise<Array<{ id: string; tool_key: string }>> }
      searchText: (row: Record<string, unknown>) => string
    }
    expect(props.allowCreate).toBe(false)
    expect(props.allowEdit).toBe(false)
    expect(props.allowArchive).toBe(false)
    expect(props.allowTrash).toBe(false)
    expect(props.toolbarActions.map((action) => action.label)).toEqual(['Todas', 'Activas', 'Disabled', 'Deprecated'])

    const rows = await props.dataSource.list()
    expect(rows[0]).toMatchObject({ id: '11111111-1111-4111-8111-111111111111', tool_key: 'medical.patient.read' })
    expect(props.searchText(rows[0])).toContain('medical.patient.read')
    expect(props.searchText(rows[0])).toContain('medical.case.review')
  })

  it('loads filtered tools by status through the shared toolbar actions', async () => {
    crudPageProps.length = 0

    render(<AdminControlCenter orgId="org-a" tenantId="tenant-a" />)

    const props = crudPageProps.at(-1) as {
      toolbarActions: Array<{ label: string; onClick: () => void }>
    }
    props.toolbarActions.find((action) => action.label === 'Disabled')?.onClick()

    await waitFor(() => {
      const nextProps = crudPageProps.at(-1) as {
        dataSource: { list: () => Promise<unknown> }
      }
      expect(nextProps).toBeTruthy()
    })
    const nextProps = crudPageProps.at(-1) as {
      dataSource: { list: () => Promise<unknown> }
    }
    await nextProps.dataSource.list()

    expect(listTools).toHaveBeenLastCalledWith('org-a', 'disabled', 'tenant-a')
  })
})
