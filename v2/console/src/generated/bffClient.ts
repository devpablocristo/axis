import createClient, { type Middleware } from 'openapi-fetch'

import type { paths } from './bffFacade'

export type BffFacadeClient = ReturnType<typeof createBffFacadeClient>

export type BffFacadeClientOptions = {
  baseUrl?: string
  apiKey?: string
  fetch?: typeof globalThis.fetch
}

/**
 * Typed client for the stable product-facing BFF facade.
 *
 * Console administration screens still use the legacy `/api` compatibility
 * adapter while those routes are migrated into the facade contract. New
 * product integrations must use this client instead of service-specific URLs.
 */
export function createBffFacadeClient(options: BffFacadeClientOptions = {}) {
  const client = createClient<paths>({
    baseUrl: options.baseUrl ?? '',
    fetch: options.fetch,
  })

  if (options.apiKey) {
    const authentication: Middleware = {
      async onRequest({ request }) {
        request.headers.set('Authorization', `Bearer ${options.apiKey}`)
        return request
      },
    }
    client.use(authentication)
  }

  return client
}
