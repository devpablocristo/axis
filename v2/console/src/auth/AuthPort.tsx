import type { ComponentType, ReactNode } from 'react'

export type AuthenticatedConsole = {
  accountSlot?: ReactNode
}

export type AuthBoundaryProps = {
  children: (session: AuthenticatedConsole) => ReactNode
}

export type AuthPort = {
  enabled: boolean
  Boundary: ComponentType<AuthBoundaryProps>
}

function AnonymousAuthBoundary({ children }: AuthBoundaryProps) {
  return children({})
}

export const anonymousAuthPort: AuthPort = {
  enabled: false,
  Boundary: AnonymousAuthBoundary,
}

export function AuthGateway({
  port,
  children,
}: AuthBoundaryProps & { port: AuthPort }) {
  const Boundary = port.Boundary
  return <Boundary>{children}</Boundary>
}

export function AuthLoadingView() {
  return (
    <main className="auth-page">
      <section className="auth-panel">
        <AxisAuthMark />
        <h1>Axis Console</h1>
      </section>
    </main>
  )
}

export function AxisAuthMark() {
  return (
    <div className="auth-mark" aria-hidden="true">
      <span />
    </div>
  )
}
