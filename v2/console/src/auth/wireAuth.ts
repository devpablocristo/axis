import { anonymousAuthPort, type AuthPort } from './AuthPort'
import { createClerkAuthAdapter } from './clerkAuthAdapter'

const publishableKey = (import.meta.env.VITE_CLERK_PUBLISHABLE_KEY ?? '').trim()
const jwtTemplate = (import.meta.env.VITE_CLERK_JWT_TEMPLATE ?? 'axis-bff').trim() || 'axis-bff'

export const configuredAuthPort: AuthPort = publishableKey
  ? createClerkAuthAdapter({ publishableKey, jwtTemplate })
  : anonymousAuthPort
