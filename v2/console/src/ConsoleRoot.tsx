import { Component, type ErrorInfo, type ReactNode } from 'react'
import { App } from './App'

class ConsoleErrorBoundary extends Component<{ children: ReactNode }, { message: string }> {
  state = { message: '' }

  static getDerivedStateFromError(error: unknown) {
    return { message: error instanceof Error ? error.message : 'Error inesperado al cargar Console' }
  }

  componentDidCatch(error: unknown, info: ErrorInfo) {
    console.error('axis_console_v2_render_failed', error, info)
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

function ShieldLogo() {
  return (
    <div className="auth-mark" aria-hidden="true">
      <span />
    </div>
  )
}

export function ConsoleRoot() {
  return (
    <ConsoleErrorBoundary>
      <App />
    </ConsoleErrorBoundary>
  )
}
