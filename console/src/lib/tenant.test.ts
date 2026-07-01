import { describe, it, expect } from 'vitest'
import { deriveTenantId, workspaceOrgs, workspaceProducts, preferred } from './tenant'

const tenants = [
  { id: '11111111-1111-4111-8111-111111111111', org_id: 'cristo.tech', product_surface: 'axis' },
  { id: '22222222-2222-4222-8222-222222222222', org_id: 'cristo.tech', product_surface: 'medmory' },
  { id: '33333333-3333-4333-8333-333333333333', org_id: 'soalen', product_surface: 'ponti' },
]

describe('tenant derivations', () => {
  it('derives tenant id from (org, product); no match → empty', () => {
    expect(deriveTenantId(tenants, 'cristo.tech', 'axis')).toBe('11111111-1111-4111-8111-111111111111')
    expect(deriveTenantId(tenants, 'cristo.tech', 'medmory')).toBe('22222222-2222-4222-8222-222222222222')
    expect(deriveTenantId(tenants, 'cristo.tech', 'nope')).toBe('')
    expect(deriveTenantId(undefined, 'x', 'y')).toBe('')
  })

  it('does not derive non-UUID tenant ids', () => {
    expect(deriveTenantId([{ id: 'medmory', org_id: 'cristo.tech', product_surface: 'medmory' }], 'cristo.tech', 'medmory')).toBe('')
  })

  it('workspaceOrgs are unique companies', () => {
    expect(workspaceOrgs(tenants)).toEqual(['cristo.tech', 'soalen'])
    expect(workspaceOrgs(undefined)).toEqual([])
  })

  it('workspaceProducts are filtered by the selected org', () => {
    expect(workspaceProducts(tenants, 'cristo.tech')).toEqual(['axis', 'medmory'])
    expect(workspaceProducts(tenants, 'soalen')).toEqual(['ponti'])
    expect(workspaceProducts(tenants, 'unknown')).toEqual([])
  })

  it('preferred picks the preferred option, else the first, else undefined', () => {
    expect(preferred(['axis', 'medmory'], 'axis')).toBe('axis')
    expect(preferred(['medmory', 'ponti'], 'axis')).toBe('medmory')
    expect(preferred([], 'axis')).toBeUndefined()
  })
})
