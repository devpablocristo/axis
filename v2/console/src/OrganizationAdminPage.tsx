import { useState } from 'react'
import { OrgsPage } from './OrgsPage'
import { ProductsPage } from './ProductsPage'
import { ProductIntegrationPage } from './ProductIntegrationPage'
import { UsersPage } from './UsersPage'

type OrganizationAdminView = 'users' | 'orgs' | 'products' | 'integration'

type OrganizationAdminPageProps = {
  organizationId: string
  principalId: string
  productId: string
  productSurface: string
  onSessionChanged: () => void | Promise<void>
}

export function OrganizationAdminPage({ organizationId, principalId, productId, productSurface, onSessionChanged }: OrganizationAdminPageProps) {
  const [view, setView] = useState<OrganizationAdminView>('users')

  return (
    <div className="organization-admin-section">
      <div className="organization-admin-section__tabs" role="tablist" aria-label="Admin section">
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
        <button
          type="button"
          role="tab"
          aria-selected={view === 'integration'}
          className={view === 'integration' ? 'btn-primary active' : 'btn-secondary'}
          onClick={() => setView('integration')}
        >
          Product integration
        </button>
      </div>

      {view === 'orgs' ? (
        <OrgsPage principalId={principalId} onSessionChanged={onSessionChanged} />
      ) : view === 'products' ? (
        <ProductsPage organizationId={organizationId} principalId={principalId} onSessionChanged={onSessionChanged} />
      ) : view === 'integration' ? (
        <ProductIntegrationPage organizationId={organizationId} productId={productId} productSurface={productSurface} principalId={principalId} />
      ) : (
        <UsersPage orgId={organizationId} principalId={principalId} />
      )}
    </div>
  )
}
