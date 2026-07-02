import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'

// Count CrudPage mounts: it remounts (via its `key`) whenever reloadVersion or
// the list query change, so a mount bump == a refetch.
const hoisted = vi.hoisted(() => ({
  mounts: 0,
  requests: [] as Array<{ url: string, init?: unknown }>,
  failNextRequest: false,
}))

type MockCrudColumn = {
  key: string
  render?: (value: unknown, row: Record<string, unknown>) => ReactNode
}

type MockCrudPageProps = {
  basePath?: string
  columns: MockCrudColumn[]
  listHeaderInlineSlot?: () => ReactNode
  onMutationSuccess?: () => Promise<void> | void
}

vi.mock('@devpablocristo/platform-crud-ui', async () => {
  const React = await import('react')
  return {
    crudStringsEs: {},
    CrudPage: (props: MockCrudPageProps) => {
      React.useEffect(() => {
        hoisted.mounts++
      }, [])
      const row: Record<string, unknown> = props.basePath?.endsWith('/users')
        ? { id: 'user-1', email: 'user@example.com', role: 'member', status: 'active' }
        : { id: 'org-1', name: 'Cristo Tech', status: 'active' }
      return React.createElement(
        'div',
        { 'data-testid': 'crudpage' },
        props.listHeaderInlineSlot?.(),
        React.createElement('button', { type: 'button', onClick: () => props.onMutationSuccess?.() }, 'Mutacion interna'),
        React.createElement(
          'div',
          { 'data-testid': `row-${row.id}` },
          props.columns.map((column) => React.createElement(
            'span',
            { key: column.key },
            column.render ? column.render(row[column.key], row) : String(row[column.key] ?? ''),
          )),
        ),
      )
    },
  }
})

vi.mock('../api', () => ({
  axisCrudHttpClient: () => ({
    json: async (url: string, init?: unknown) => {
      hoisted.requests.push({ url, init })
      if (hoisted.failNextRequest) {
        hoisted.failNextRequest = false
        throw new Error('network down')
      }
      return { items: [] }
    },
  }),
  listIAMTenants: async () => [],
}))

import { IAMControlCenter } from '../IAMControlCenter'

const baseProps = {
  orgId: 'cristo.tech',
  tenantId: 'tenant-axis',
  orgs: [],
  onOrgChange: () => {},
  onRefreshShell: async () => {},
}

beforeEach(() => {
  hoisted.mounts = 0
  hoisted.requests = []
  hoisted.failNextRequest = false
  localStorage.clear()
})

describe('IAMControlCenter', () => {
  it('persists the active tab to localStorage', () => {
    render(<IAMControlCenter {...baseProps} productSurface="axis" />)
    // Default tab is "Orgs"; switch to Usuarios.
    fireEvent.click(screen.getByRole('button', { name: 'Usuarios' }))
    expect(localStorage.getItem('axis.iam.tab')).toBe('users')
  })

  it('keeps the active tab and refetches when the tenant changes', () => {
    localStorage.setItem('axis.iam.tab', 'users')
    const { rerender } = render(<IAMControlCenter {...baseProps} productSurface="axis" />)
    expect(screen.getByRole('button', { name: 'Usuarios' })).toHaveClass('active')

    const before = hoisted.mounts
    rerender(<IAMControlCenter {...baseProps} tenantId="tenant-medmory" productSurface="medmory" />)

    // Tab must NOT bounce back to "Orgs"…
    expect(screen.getByRole('button', { name: 'Usuarios' })).toHaveClass('active')
    // …and the list must refetch for the new tenant.
    expect(hoisted.mounts).toBeGreaterThan(before)
  })

  it('refreshes the shell after a successful bulk mutation', async () => {
    const onRefreshShell = vi.fn(async () => {})
    render(<IAMControlCenter {...baseProps} productSurface="axis" onRefreshShell={onRefreshShell} />)

    fireEvent.click(screen.getByLabelText('Seleccionar org-1'))
    fireEvent.click(screen.getByRole('button', { name: 'Archivar' }))

    await waitFor(() => expect(onRefreshShell).toHaveBeenCalledTimes(1))
    expect(hoisted.requests).toEqual([
      { url: '/api/iam/tenants/org-1/archive', init: { method: 'POST', body: {} } },
    ])
  })

  it('shows an inline error when a bulk mutation fails', async () => {
    const onRefreshShell = vi.fn(async () => {})
    hoisted.failNextRequest = true
    render(<IAMControlCenter {...baseProps} productSurface="axis" onRefreshShell={onRefreshShell} />)

    fireEvent.click(screen.getByLabelText('Seleccionar org-1'))
    fireEvent.click(screen.getByRole('button', { name: 'Archivar' }))

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('network down'))
    expect(onRefreshShell).not.toHaveBeenCalled()
    expect(screen.getByLabelText('Seleccionar org-1')).toBeChecked()
  })

  it('refreshes the shell after a successful CrudPage mutation', async () => {
    const onRefreshShell = vi.fn(async () => {})
    render(<IAMControlCenter {...baseProps} productSurface="axis" onRefreshShell={onRefreshShell} />)

    const before = hoisted.mounts
    fireEvent.click(screen.getByRole('button', { name: 'Mutacion interna' }))

    await waitFor(() => expect(onRefreshShell).toHaveBeenCalledTimes(1))
    expect(hoisted.mounts).toBeGreaterThan(before)
  })
})
