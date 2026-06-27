import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'

// Count CrudPage mounts: it remounts (via its `key`) whenever reloadVersion or
// the list query change, so a mount bump == a refetch.
const hoisted = vi.hoisted(() => ({ mounts: 0 }))

vi.mock('@devpablocristo/platform-crud-ui', async () => {
  const React = await import('react')
  return {
    crudStringsEs: {},
    CrudPage: () => {
      React.useEffect(() => {
        hoisted.mounts++
      }, [])
      return React.createElement('div', { 'data-testid': 'crudpage' })
    },
  }
})

vi.mock('../api', () => ({
  axisCrudHttpClient: () => ({ json: async () => ({ items: [] }) }),
  listIAMTenants: async () => [],
}))

import { IAMControlCenter } from '../IAMControlCenter'

const baseProps = {
  orgId: 'cristo.tech',
  orgs: [],
  onOrgChange: () => {},
  onRefreshShell: async () => {},
}

beforeEach(() => {
  hoisted.mounts = 0
  localStorage.clear()
})

describe('IAMControlCenter', () => {
  it('persists the active tab to localStorage', () => {
    render(<IAMControlCenter {...baseProps} productSurface="axis" />)
    // Default tab is "Orgs"; switch to Usuarios.
    fireEvent.click(screen.getByRole('button', { name: 'Usuarios' }))
    expect(localStorage.getItem('axis.iam.tab')).toBe('users')
  })

  it('keeps the active tab and refetches when the product changes', () => {
    localStorage.setItem('axis.iam.tab', 'users')
    const { rerender } = render(<IAMControlCenter {...baseProps} productSurface="axis" />)
    expect(screen.getByRole('button', { name: 'Usuarios' })).toHaveClass('active')

    const before = hoisted.mounts
    rerender(<IAMControlCenter {...baseProps} productSurface="medmory" />)

    // Tab must NOT bounce back to "Orgs"…
    expect(screen.getByRole('button', { name: 'Usuarios' })).toHaveClass('active')
    // …and the list must refetch for the new tenant.
    expect(hoisted.mounts).toBeGreaterThan(before)
  })
})
