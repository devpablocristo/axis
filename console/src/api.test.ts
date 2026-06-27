import { beforeEach, describe, expect, it, vi } from 'vitest'
import { axisFetch } from './api'

function jsonResponse(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('axisFetch', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.restoreAllMocks()
  })

  it('sends the explicit tenant header for scoped requests', async () => {
    const fetchMock = vi.fn(async () => jsonResponse({ ok: true }))
    vi.stubGlobal('fetch', fetchMock)
    localStorage.setItem('axis.tenant_id', 'tenant-stale')

    await axisFetch('/api/companion/v1/tasks', 'org-a', { tenantId: 'tenant-active' })

    const [, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    const headers = new Headers(init.headers)
    expect(headers.get('X-Axis-Org-ID')).toBe('org-a')
    expect(headers.get('X-Tenant-ID')).toBe('tenant-active')
  })

  it('can intentionally omit tenant header for bootstrap requests', async () => {
    const fetchMock = vi.fn(async () => jsonResponse({ ok: true }))
    vi.stubGlobal('fetch', fetchMock)
    localStorage.setItem('axis.tenant_id', 'tenant-stale')

    await axisFetch('/api/session', '', { tenantId: null })

    const [, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    const headers = new Headers(init.headers)
    expect(headers.has('X-Tenant-ID')).toBe(false)
  })
})
