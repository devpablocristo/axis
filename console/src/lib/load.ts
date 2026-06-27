export type LoadState<T> = {
  data: T
  error: string
  loading: boolean
}

export const empty = <T,>(data: T): LoadState<T> => ({ data, error: '', loading: false })

// load runs an async fetch into a LoadState setter. It KEEPS the previously
// loaded data visible while refreshing (and on error). Clearing it to the
// fallback mid-refresh makes derived flags (scopes → canViewIAM/canViewControl,
// tenants → active tenant) flip to empty for a tick, which unmounts whole
// screens and resets their local state (e.g. the IAM tab jumps back to "tenants").
export async function load<T>(
  setState: (value: LoadState<T> | ((prev: LoadState<T>) => LoadState<T>)) => void,
  fn: () => Promise<T>,
  fallback: T,
) {
  setState((prev) => ({ data: prev?.data ?? fallback, error: '', loading: true }))
  try {
    const data = await fn()
    setState({ data, error: '', loading: false })
  } catch (error) {
    setState((prev) => ({ data: prev?.data ?? fallback, error: error instanceof Error ? error.message : 'error', loading: false }))
  }
}
