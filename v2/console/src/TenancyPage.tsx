import { useState } from 'react'
import { OrgsPage } from './OrgsPage'
import { ProductsPage } from './ProductsPage'
import { TenantsPage } from './TenantsPage'
import type { Tenant } from './api'

type TenancyView = 'tenants' | 'orgs' | 'products'

type TenancyPageProps = {
  principalId: string
  sessionTenants: Tenant[]
  onSessionChanged: () => void | Promise<void>
}

export function TenancyPage({ principalId, sessionTenants, onSessionChanged }: TenancyPageProps) {
  const [view, setView] = useState<TenancyView>('tenants')

  return (
    <div className="tenancy-section">
      <div className="tenancy-section__tabs" role="tablist" aria-label="Tenants section">
        <button
          type="button"
          className={view === 'tenants' ? 'active' : ''}
          onClick={() => setView('tenants')}
        >
          Tenants
        </button>
        <button
          type="button"
          className={view === 'orgs' ? 'active' : ''}
          onClick={() => setView('orgs')}
        >
          Orgs
        </button>
        <button
          type="button"
          className={view === 'products' ? 'active' : ''}
          onClick={() => setView('products')}
        >
          Products
        </button>
      </div>

      {view === 'orgs' ? (
        <OrgsPage principalId={principalId} onSessionChanged={onSessionChanged} />
      ) : view === 'products' ? (
        <ProductsPage principalId={principalId} onSessionChanged={onSessionChanged} />
      ) : (
        <TenantsPage principalId={principalId} sessionTenants={sessionTenants} onSessionChanged={onSessionChanged} />
      )}
    </div>
  )
}
