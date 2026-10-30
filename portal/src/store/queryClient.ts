import { MutationCache, QueryCache, QueryClient } from '@tanstack/react-query'
import { ApiError } from '../api'

let redirecting = false

function redirectTo(to: string) {
  if (redirecting) return
  redirecting = true
  window.location.assign(to)
}

/**
 * Where a failed request should send the browser, if anywhere.
 *
 * Session and role failures can come from any query or mutation, not just the
 * initial `me` probe, so the decision lives here and is applied once in
 * `handleAPIError`. Login and registration failures stay on their form.
 */
export function authRedirectTarget(
  error: unknown,
  pathname: string,
  search = '',
): string | undefined {
  if (!(error instanceof ApiError)) return undefined
  // Public pages probe the session to show the right navigation, but a 401
  // there only means "not signed in" and must not bounce visitors to login.
  const protectedPage =
    pathname === '/portal' ||
    pathname.startsWith('/portal/') ||
    pathname === '/admin' ||
    pathname.startsWith('/admin/')
  if (error.status === 401 && !error.authRequest && protectedPage) {
    return `/login?redirect=${encodeURIComponent(pathname + search)}`
  }
  if (error.status === 403 && error.code === 'ADMIN_REQUIRED' && pathname.startsWith('/admin')) {
    return '/portal'
  }
  return undefined
}

// Handle a session or role failure once so an expired session never leaves
// stale tenant data on screen.
function handleAPIError(error: unknown) {
  const target = authRedirectTarget(error, window.location.pathname, window.location.search)
  if (!target) return
  if (target.startsWith('/login')) queryClient.clear()
  redirectTo(target)
}

// Server state (session, devices, apps) lives in TanStack Query. There is no
// separate global store: everything the console shows comes from the portal
// API and is invalidated after mutations.
export const queryClient = new QueryClient({
  queryCache: new QueryCache({ onError: handleAPIError }),
  mutationCache: new MutationCache({ onError: handleAPIError }),
  defaultOptions: {
    queries: {
      retry: (failureCount, error) => {
        if (error instanceof ApiError && error.status < 500) {
          return false
        }
        return failureCount < 1
      },
      refetchOnWindowFocus: false,
    },
  },
})
