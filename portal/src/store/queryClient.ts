import { MutationCache, QueryCache, QueryClient } from '@tanstack/react-query'
import { ApiError } from '../api'

let redirecting = false

function redirectTo(to: string) {
  if (redirecting) return
  redirecting = true
  window.location.assign(to)
}

// Session and role failures can come from any query or mutation, not just the
// initial `me` probe. Handle them once here so an expired session never leaves
// stale tenant data on screen.
function handleAPIError(error: unknown) {
  if (!(error instanceof ApiError)) return
  const path = window.location.pathname
  // Public pages probe the session to show the right navigation, but a 401
  // there only means "not signed in" and must not bounce visitors to login.
  const protectedPage = path === '/portal' || path.startsWith('/portal/') || path === '/admin' || path.startsWith('/admin/')
  if (error.status === 401 && !error.authRequest && protectedPage) {
    queryClient.clear()
    const next = window.location.pathname + window.location.search
    redirectTo(`/login?redirect=${encodeURIComponent(next)}`)
    return
  }
  if (error.status === 403 && error.code === 'ADMIN_REQUIRED') {
    if (window.location.pathname.startsWith('/admin')) {
      redirectTo('/portal')
    }
  }
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
