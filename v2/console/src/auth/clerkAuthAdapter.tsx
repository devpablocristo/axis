import {
  ClerkProvider,
  SignedIn,
  SignedOut,
  SignInButton,
  UserButton,
  useAuth,
} from '@clerk/clerk-react'
import { useLayoutEffect } from 'react'
import { setAxisAuthTokenGetter } from '../api'
import {
  AuthLoadingView,
  AxisAuthMark,
  type AuthBoundaryProps,
  type AuthPort,
} from './AuthPort'

type ClerkAuthAdapterOptions = {
  publishableKey: string
  jwtTemplate: string
}

export function createClerkAuthAdapter({
  publishableKey,
  jwtTemplate,
}: ClerkAuthAdapterOptions): AuthPort {
  function ClerkAuthenticatedConsole({ children }: AuthBoundaryProps) {
    const { getToken, isLoaded, isSignedIn } = useAuth()

    useLayoutEffect(() => {
      if (!isLoaded || !isSignedIn) {
        setAxisAuthTokenGetter(null)
        return undefined
      }
      setAxisAuthTokenGetter(() => getToken({ template: jwtTemplate }))
      return () => setAxisAuthTokenGetter(null)
    }, [getToken, isLoaded, isSignedIn])

    if (!isLoaded || !isSignedIn) return <AuthLoadingView />
    return children({ accountSlot: <UserButton /> })
  }

  function ClerkAuthBoundary({ children }: AuthBoundaryProps) {
    return (
      <ClerkProvider publishableKey={publishableKey}>
        <SignedIn>
          <ClerkAuthenticatedConsole>{children}</ClerkAuthenticatedConsole>
        </SignedIn>
        <SignedOut>
          <main className="auth-page">
            <section className="auth-panel">
              <AxisAuthMark />
              <h1>Axis Console</h1>
              <SignInButton mode="redirect" forceRedirectUrl="/" fallbackRedirectUrl="/">
                <button type="button" className="btn-primary">Sign in</button>
              </SignInButton>
            </section>
          </main>
        </SignedOut>
      </ClerkProvider>
    )
  }

  return {
    enabled: true,
    Boundary: ClerkAuthBoundary,
  }
}
