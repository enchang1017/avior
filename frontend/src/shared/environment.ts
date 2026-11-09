import type { CurrentUser } from './api'

type Environment = CurrentUser['environment']

/**
 * The tint an environment lends to whatever is pointed at it: the editor's tab,
 * a connection's badge. Production is red, staging blue, anything else neutral.
 *
 * Written out in full because Tailwind reads the source for the classes it
 * emits, and one assembled from the environment's name is one it never sees.
 */
const tabs: Record<Environment, string> = {
  production: 'border-t-env-production bg-env-production-wash text-env-production',
  staging: 'border-t-env-staging bg-env-staging-wash text-env-staging',
  development: 'border-t-env-local bg-env-local-wash text-fg',
  test: 'border-t-env-local bg-env-local-wash text-fg',
}

export function environmentTab(environment: Environment): string {
  return tabs[environment]
}
