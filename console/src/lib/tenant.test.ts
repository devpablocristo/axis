import { describe, it, expect } from 'vitest'
import { deriveTenantId, workspaceOrgs, workspaceProducts, preferred } from './tenant'

const tenants = [
  { id: 'tn_a', org_id: 'cristo.tech', product_surface: 'axis' },
  { id: 'tn_m', org_id: 'cristo.tech', product_surface: 'medmory' },
  { id: 'tn_s', org_id: 'soalen', product_surface: 'ponti' },
]

describe('tenant derivations', () => {
  it('derives tenant id from (org, product); no match → empty', () => {
    expect(deriveTenantId(tenants, 'cristo.tech', 'axis')).toBe('tn_a')
    expect(deriveTenantId(tenants, 'cristo.tech', 'medmory')).toBe('tn_m')
    expect(deriveTenantId(tenants, 'cristo.tech', 'nope')).toBe('')
    expect(deriveTenantId(undefined, 'x', 'y')).toBe('')
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
