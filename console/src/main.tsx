import React, { useEffect } from 'react'
import { createRoot } from 'react-dom/client'
import { ClerkProvider, SignedIn, SignedOut, SignInButton, UserButton, useAuth } from '@clerk/clerk-react'
import { App } from './App'
import { setAxisAuthTokenGetter } from './api'
import './styles.css'

const clerkPublishableKey = (import.meta.env.VITE_CLERK_PUBLISHABLE_KEY ?? '').trim()

function ClerkTokenBridge() {
  const { getToken, isLoaded, isSignedIn } = useAuth()

  useEffect(() => {
    if (!isLoaded || !isSignedIn) {
      setAxisAuthTokenGetter(null)
      return
    }
    setAxisAuthTokenGetter(() => getToken())
    return () => setAxisAuthTokenGetter(null)
  }, [getToken, isLoaded, isSignedIn])

  return null
}

function AxisConsoleWithClerk() {
  return (
    <>
      <SignedIn>
        <ClerkTokenBridge />
        <App authSlot={<UserButton />} />
      </SignedIn>
      <SignedOut>
        <main className="auth-page">
          <section className="auth-panel">
            <ShieldLogo />
            <h1>Axis Console</h1>
            <SignInButton mode="modal">
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
    {app}
  </React.StrictMode>
)
