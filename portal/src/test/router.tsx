import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from '@tanstack/react-router'
import { render, waitFor, type RenderResult } from '@testing-library/react'
import type { ReactNode } from 'react'
import { ThemeProvider } from '../components/ThemeProvider'
import { createAppRouter } from '../router'

export function makeQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
        refetchOnWindowFocus: false,
        // Tests drive refresh explicitly instead of waiting for timers.
        staleTime: 60_000,
      },
      mutations: { retry: false },
    },
  })
}

interface ProvidersProps {
  queryClient: QueryClient
  children: ReactNode
}

function Providers({ queryClient, children }: ProvidersProps) {
  return (
    <ThemeProvider>
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    </ThemeProvider>
  )
}

export interface AppRender extends RenderResult {
  router: ReturnType<typeof createAppRouter>
  queryClient: QueryClient
}

/** Render the real application router at `path`. */
export async function renderApp(path = '/'): Promise<AppRender> {
  const queryClient = makeQueryClient()
  const router = createAppRouter(queryClient, createMemoryHistory({ initialEntries: [path] }))
  // Resolve the first match before painting, so tests do not race the router's
  // own load effect.
  await router.load()
  const result = render(
    <Providers queryClient={queryClient}>
      <RouterProvider router={router} />
    </Providers>,
  )
  return { ...result, router, queryClient }
}

// Every link target the components use. They render nothing; they exist so a
// component can be mounted on its own without the real page tree.
const linkTargets = [
  '/',
  '/login',
  '/register',
  '/portal',
  '/portal/agents',
  '/portal/devices',
  '/portal/apps',
  '/portal/apps/$appId',
  '/portal/screens',
  '/portal/services',
  '/portal/settings',
  '/portal/activity',
  '/portal/call',
  '/admin',
  '/admin/users',
  '/admin/users/$userId',
  '/admin/agents',
  '/admin/apps',
  '/admin/settings',
]

export interface ComponentRender extends RenderResult {
  router: ReturnType<typeof createRouter>
  queryClient: QueryClient
  mountPath: string
}

/**
 * Mount a component that uses router links without booting the whole app.
 * Every link target is registered so `Link` can resolve; only the mount route
 * renders anything.
 */
export async function renderInRouter(ui: ReactNode): Promise<ComponentRender> {
  const queryClient = makeQueryClient()
  const mountPath = '/__mount'
  const rootRoute = createRootRoute({ component: () => <Outlet /> })
  const mountRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: mountPath,
    component: () => <>{ui}</>,
  })
  const targets = linkTargets.map((path) =>
    createRoute({
      getParentRoute: () => rootRoute,
      path,
      component: () => null,
    }),
  )
  const routeTree = rootRoute.addChildren([mountRoute, ...targets])
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [mountPath] }),
  })
  await router.load()
  const result = render(
    <Providers queryClient={queryClient}>
      <RouterProvider router={router} />
    </Providers>,
  )
  return { ...result, router: router as ReturnType<typeof createRouter>, queryClient, mountPath }
}

/** Advance until the router has settled on `path`. */
export async function settledAt(
  router: { state: { location: { pathname: string } } },
  path: string,
): Promise<void> {
  await waitFor(() => {
    if (router.state.location.pathname !== path) {
      throw new Error(`router is at ${router.state.location.pathname}, expected ${path}`)
    }
  })
}
