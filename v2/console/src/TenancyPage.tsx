import { useState } from 'react'
import { OrgsPage } from './OrgsPage'
import { ProductsPage } from './ProductsPage'
import { TenantsPage } from './TenantsPage'
import { UsersPage } from './UsersPage'
import type { Tenant } from './api'

type TenancyView = 'users' | 'tenants' | 'orgs' | 'products'

type TenancyPageProps = {
  tenantId: string
  principalId: string
  sessionTenants: Tenant[]
  onSessionChanged: () => void | Promise<void>
}

export function TenancyPage({ tenantId, principalId, sessionTenants, onSessionChanged }: TenancyPageProps) {
  const [view, setView] = useState<TenancyView>('users')

  return (
    <div className="tenancy-section">
      <div className="tenancy-section__tabs" role="tablist" aria-label="Admin section">
        <button
          type="button"
          role="tab"
          aria-selected={view === 'users'}
          className={view === 'users' ? 'btn-primary active' : 'btn-secondary'}
          onClick={() => setView('users')}
        >
          Users
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={view === 'tenants'}
          className={view === 'tenants' ? 'btn-primary active' : 'btn-secondary'}
          onClick={() => setView('tenants')}
        >
          Tenants
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={view === 'orgs'}
          className={view === 'orgs' ? 'btn-primary active' : 'btn-secondary'}
          onClick={() => setView('orgs')}
        >
          Orgs
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={view === 'products'}
          className={view === 'products' ? 'btn-primary active' : 'btn-secondary'}
          onClick={() => setView('products')}
        >
          Products
        </button>
      </div>

      {view === 'orgs' ? (
        <OrgsPage principalId={principalId} onSessionChanged={onSessionChanged} />
      ) : view === 'products' ? (
        <ProductsPage principalId={principalId} onSessionChanged={onSessionChanged} />
      ) : view === 'tenants' ? (
        <TenantsPage principalId={principalId} sessionTenants={sessionTenants} onSessionChanged={onSessionChanged} />
      ) : (
        <UsersPage tenantId={tenantId} principalId={principalId} />
      )}
    </div>
  )
}
