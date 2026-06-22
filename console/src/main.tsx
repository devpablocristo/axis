import React, { Component, type ErrorInfo, type ReactNode, useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { ClerkProvider, SignedIn, SignedOut, SignInButton, UserButton, useAuth } from '@clerk/clerk-react'
import { App } from './App'
import { setAxisAuthTokenGetter } from './api'
import './styles.css'

const clerkPublishableKey = (import.meta.env.VITE_CLERK_PUBLISHABLE_KEY ?? '').trim()
const clerkJwtTemplate = (import.meta.env.VITE_CLERK_JWT_TEMPLATE ?? 'axis-bff').trim()

class ConsoleErrorBoundary extends Component<{ children: ReactNode }, { message: string }> {
  state = { message: '' }

  static getDerivedStateFromError(error: unknown) {
    return { message: error instanceof Error ? error.message : 'Error inesperado al cargar Console' }
  }

  componentDidCatch(error: unknown, info: ErrorInfo) {
    console.error('axis_console_render_failed', error, info)
  }

  render() {
    if (this.state.message) {
      return (
        <main className="auth-page">
          <section className="auth-panel">
            <ShieldLogo />
            <h1>Axis Console</h1>
            <p>{this.state.message}</p>
            <button type="button" onClick={() => window.location.reload()}>Recargar</button>
          </section>
        </main>
      )
    }
    return this.props.children
  }
}

function AxisSignedInConsole() {
  const { getToken, isLoaded, isSignedIn } = useAuth()
  const [authReady, setAuthReady] = useState(false)

  useEffect(() => {
    if (!isLoaded || !isSignedIn) {
      setAxisAuthTokenGetter(null)
      setAuthReady(false)
      return
    }
    setAxisAuthTokenGetter(() => getToken({ template: clerkJwtTemplate || 'axis-bff' }))
    setAuthReady(true)
    return () => setAxisAuthTokenGetter(null)
  }, [getToken, isLoaded, isSignedIn])

  if (!authReady) {
    return (
      <main className="auth-page">
        <section className="auth-panel">
          <ShieldLogo />
          <h1>Axis Console</h1>
        </section>
      </main>
    )
  }

  return <App authSlot={<UserButton />} />
}

function AxisConsoleWithClerk() {
  return (
    <>
      <SignedIn>
        <AxisSignedInConsole />
      </SignedIn>
      <SignedOut>
        <main className="auth-page">
          <section className="auth-panel">
            <ShieldLogo />
            <h1>Axis Console</h1>
            <SignInButton mode="redirect" forceRedirectUrl="/" fallbackRedirectUrl="/">
              <button type="button">Ingresar</button>
            </SignInButton>
          </section>
        </main>
      </SignedOut>
    </>
  )
}

function ShieldLogo() {
  return (
    <div className="auth-mark" aria-hidden="true">
      <span />
    </div>
  )
}

const app = clerkPublishableKey ? (
  <ClerkProvider publishableKey={clerkPublishableKey}>
    <AxisConsoleWithClerk />
  </ClerkProvider>
) : (
  <App />
)

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ConsoleErrorBoundary>
      {app}
    </ConsoleErrorBoundary>
  </React.StrictMode>
)
