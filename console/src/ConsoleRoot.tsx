import { Component, type ErrorInfo, type ReactNode, useLayoutEffect } from 'react'
import { ClerkProvider, SignedIn, SignedOut, SignInButton, UserButton, useAuth } from '@clerk/clerk-react'
import { App } from './App'
import { setAxisAuthTokenGetter } from './api'

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

  useLayoutEffect(() => {
    if (!isLoaded || !isSignedIn) {
      setAxisAuthTokenGetter(null)
      return undefined
    }
    setAxisAuthTokenGetter(() => getToken({ template: clerkJwtTemplate || 'axis-bff' }))
    return () => setAxisAuthTokenGetter(null)
  }, [getToken, isLoaded, isSignedIn])

  if (!isLoaded || !isSignedIn) {
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

export function ConsoleRoot() {
  const app = clerkPublishableKey ? (
    <ClerkProvider publishableKey={clerkPublishableKey}>
      <AxisConsoleWithClerk />
    </ClerkProvider>
  ) : (
    <App />
  )

  return (
    <ConsoleErrorBoundary>
      {app}
    </ConsoleErrorBoundary>
  )
}
