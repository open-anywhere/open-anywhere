import type { QueryClient } from '@tanstack/react-query'
import {
  Outlet,
  createRootRouteWithContext,
  createRoute,
  createRouter,
  lazyRouteComponent,
  redirect,
} from '@tanstack/react-router'
import { Button, Result } from 'antd'
import { ApiError } from '../api'
import { meQueryOptions } from '../api/queries'

export interface RouterContext {
  queryClient: QueryClient
}

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: Outlet,
  notFoundComponent: NotFound,
  errorComponent: RootError,
})

function NotFound() {
  return (
    <div className="fullscreen-result">
      <Result
        status="404"
        title="页面不存在"
        subTitle="这个地址没有对应的页面。"
        extra={
          <Button type="primary" href="/portal">
            回到我的空间
          </Button>
        }
      />
    </div>
  )
}

function RoutePending() {
  return (
    <div className="route-pending" role="status" aria-label="页面加载中">
      <span className="route-pending-spinner" aria-hidden="true" />
    </div>
  )
}

function RootError({ error }: { error: unknown }) {
  const message = error instanceof Error ? error.message : String(error)
  return (
    <div className="fullscreen-result">
      <Result
        status="error"
        title="页面无法加载"
        subTitle={message || '发生了未知错误，请刷新后重试。'}
        extra={
          <Button type="primary" onClick={() => window.location.reload()}>
            刷新页面
          </Button>
        }
      />
    </div>
  )
}

const homeRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  component: lazyRouteComponent(() => import('../pages/HomePage'), 'HomePage'),
})

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/login',
  validateSearch: (
    search: Record<string, unknown>,
  ): { redirect?: string; switchAccount?: boolean } => ({
    redirect: typeof search.redirect === 'string' ? search.redirect : undefined,
    switchAccount: search.switchAccount === true || search.switchAccount === '1',
  }),
  component: lazyRouteComponent(() => import('../pages/LoginPage'), 'LoginPage'),
})

const registerRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/register',
  component: lazyRouteComponent(() => import('../pages/RegisterPage'), 'RegisterPage'),
})

async function requireAuth({
  context,
  location,
}: {
  context: RouterContext
  location: { href: string }
}) {
  try {
    await context.queryClient.ensureQueryData(meQueryOptions)
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      throw redirect({ to: '/login', search: { redirect: location.href } })
    }
    throw error
  }
}

async function requireAdmin({
  context,
}: {
  context: RouterContext
  location: { href: string }
}) {
  let me
  try {
    me = await context.queryClient.ensureQueryData(meQueryOptions)
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      throw redirect({ to: '/login' })
    }
    throw error
  }
  if (me.user.role !== 'admin') {
    throw redirect({ to: '/portal' })
  }
}

const portalRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/portal',
  component: lazyRouteComponent(() => import('../components/layout/PortalLayout'), 'PortalLayout'),
  beforeLoad: requireAuth,
})

const overviewRoute = createRoute({
  getParentRoute: () => portalRoute,
  path: '/',
  component: lazyRouteComponent(() => import('../pages/portal/OverviewPage'), 'OverviewPage'),
})

const appsRoute = createRoute({
  getParentRoute: () => portalRoute,
  path: 'apps',
  component: lazyRouteComponent(() => import('../pages/portal/AppsPage'), 'AppsPage'),
})

const appDetailRoute = createRoute({
  getParentRoute: () => portalRoute,
  path: 'apps/$appId',
  component: lazyRouteComponent(() => import('../pages/portal/AppDetailPage'), 'AppDetailPage'),
})

const screensRoute = createRoute({
  getParentRoute: () => portalRoute,
  path: 'screens',
  component: lazyRouteComponent(() => import('../pages/portal/ScreensPage'), 'ScreensPage'),
})

const devicesRoute = createRoute({
  getParentRoute: () => portalRoute,
  path: 'devices',
  component: lazyRouteComponent(() => import('../pages/portal/DevicesPage'), 'DevicesPage'),
})

const agentsRoute = createRoute({
  getParentRoute: () => portalRoute,
  path: 'agents',
  component: lazyRouteComponent(() => import('../pages/portal/AgentsPage'), 'AgentsPage'),
})

const servicesRoute = createRoute({
  getParentRoute: () => portalRoute,
  path: 'services',
  component: lazyRouteComponent(() => import('../pages/portal/ServicesPage'), 'ServicesPage'),
})

const activityRoute = createRoute({
  getParentRoute: () => portalRoute,
  path: 'activity',
  component: lazyRouteComponent(() => import('../pages/portal/ActivityPage'), 'ActivityPage'),
})

const settingsRoute = createRoute({
  getParentRoute: () => portalRoute,
  path: 'settings',
  component: lazyRouteComponent(() => import('../pages/portal/SettingsPage'), 'SettingsPage'),
})

const callRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/portal/call',
  beforeLoad: requireAuth,
  validateSearch: (search: Record<string, unknown>): { device?: string } => ({
    device: typeof search.device === 'string' ? search.device : undefined,
  }),
  component: lazyRouteComponent(() => import('../pages/portal/CallPage'), 'CallPage'),
})

const adminRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/admin',
  component: lazyRouteComponent(() => import('../components/layout/PortalLayout'), 'PortalLayout'),
  beforeLoad: requireAdmin,
})

const adminOverviewRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: '/',
  component: lazyRouteComponent(
    () => import('../pages/admin/AdminOverviewPage'),
    'AdminOverviewPage',
  ),
})

const adminUsersRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: 'users',
  component: lazyRouteComponent(() => import('../pages/admin/AdminUsersPage'), 'AdminUsersPage'),
})

const adminUserDetailRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: 'users/$userId',
  component: lazyRouteComponent(
    () => import('../pages/admin/AdminUserDetailPage'),
    'AdminUserDetailPage',
  ),
})

const adminAgentsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: 'agents',
  component: lazyRouteComponent(() => import('../pages/admin/AdminAgentsPage'), 'AdminAgentsPage'),
})

const adminAppsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: 'apps',
  component: lazyRouteComponent(() => import('../pages/admin/AdminAppsPage'), 'AdminAppsPage'),
})

const adminSettingsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: 'settings',
  component: lazyRouteComponent(
    () => import('../pages/admin/AdminSettingsPage'),
    'AdminSettingsPage',
  ),
})

function redirectToPortal() {
  throw redirect({ to: '/portal' })
}

const consoleRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/portal/console',
  beforeLoad: redirectToPortal,
})

const consoleSplatRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/portal/console/$',
  beforeLoad: redirectToPortal,
})

const legacyConsoleRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/console',
  beforeLoad: redirectToPortal,
})

const legacyConsoleSplatRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/console/$',
  beforeLoad: redirectToPortal,
})

const routeTree = rootRoute.addChildren([
  homeRoute,
  loginRoute,
  registerRoute,
  portalRoute.addChildren([
    overviewRoute,
    appsRoute,
    appDetailRoute,
    screensRoute,
    agentsRoute,
    devicesRoute,
    servicesRoute,
    activityRoute,
    settingsRoute,
  ]),
  adminRoute.addChildren([
    adminOverviewRoute,
    adminUsersRoute,
    adminUserDetailRoute,
    adminAgentsRoute,
    adminAppsRoute,
    adminSettingsRoute,
  ]),
  callRoute,
  consoleRoute,
  consoleSplatRoute,
  legacyConsoleRoute,
  legacyConsoleSplatRoute,
])

export function createAppRouter(queryClient: QueryClient) {
  return createRouter({
    routeTree,
    context: { queryClient },
    basepath: '/',
    trailingSlash: 'never',
    defaultPreload: 'intent',
    defaultPendingComponent: RoutePending,
    defaultPendingMs: 150,
    defaultPendingMinMs: 300,
    scrollRestoration: true,
  })
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof createAppRouter>
  }
}
