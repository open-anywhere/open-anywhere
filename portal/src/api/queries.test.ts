import { describe, expect, it } from 'vitest'
import {
  adminAgentsQueryOptions,
  adminAppsQueryOptions,
  adminOverviewQueryOptions,
  adminSettingsQueryOptions,
  adminUserQueryOptions,
  adminUsersQueryOptions,
  appQueryOptions,
  appsQueryOptions,
  meQueryOptions,
  screensQueryOptions,
  servicesQueryOptions,
} from './queries'
import { mockApi, ok } from '../test/http'
import { makeAdminAgent, makeAdminUser, makeAppDetail } from '../test/fixtures'

function intervalOf(options: { refetchInterval?: unknown }): (
  query: unknown,
) => number | false | undefined {
  return options.refetchInterval as (query: unknown) => number | false | undefined
}

describe('query keys and polling', () => {
  it('identifies each resource', () => {
    expect(meQueryOptions.queryKey).toEqual(['me'])
    expect(servicesQueryOptions.queryKey).toEqual(['services'])
    expect(appsQueryOptions.queryKey).toEqual(['apps'])
    expect(screensQueryOptions.queryKey).toEqual(['screens'])
    expect(appQueryOptions('app_1').queryKey).toEqual(['app', 'app_1'])
    expect(adminOverviewQueryOptions.queryKey).toEqual(['admin', 'overview'])
    expect(adminSettingsQueryOptions.queryKey).toEqual(['admin', 'settings'])
    expect(adminUserQueryOptions('u_1').queryKey).toEqual(['admin', 'user', 'u_1'])
    expect(adminUsersQueryOptions({ query: 'a' }).queryKey).toEqual(['admin', 'users', { query: 'a' }])
    expect(adminAgentsQueryOptions({ state: 'error' }).queryKey).toEqual([
      'admin',
      'agents',
      { state: 'error' },
    ])
    expect(adminAppsQueryOptions({}).queryKey).toEqual(['admin', 'apps', {}])
  })

  it('polls screens and admin summaries on a fixed interval', () => {
    expect(screensQueryOptions.refetchInterval).toBe(10_000)
    expect(adminOverviewQueryOptions.refetchInterval).toBe(10_000)
    expect(adminSettingsQueryOptions.refetchInterval).toBe(10_000)
    expect(adminAppsQueryOptions({}).refetchInterval).toBe(10_000)
    expect(adminUsersQueryOptions({}).refetchInterval).toBe(10_000)
  })

  it('never retries a failed query', () => {
    for (const options of [
      meQueryOptions,
      servicesQueryOptions,
      appsQueryOptions,
      screensQueryOptions,
      adminOverviewQueryOptions,
    ]) {
      expect(options.retry).toBe(false)
    }
  })

  it('polls an agent while it is still provisioning', () => {
    const detail = intervalOf(adminUserQueryOptions('u_1'))
    expect(
      detail({ state: { data: { user: makeAdminUser({ agents: [makeAdminAgent({ state: 'provisioning' })] }) } } }),
    ).toBe(2_000)
    expect(
      detail({ state: { data: { user: makeAdminUser({ agents: [makeAdminAgent()] }) } } }),
    ).toBe(10_000)
    expect(detail({ state: {} })).toBe(10_000)
  })

  it('polls the agent list faster while a device is provisioning', () => {
    const interval = intervalOf(adminAgentsQueryOptions({}))
    expect(interval({ state: { data: { agents: [makeAdminAgent({ state: 'provisioning' })] } } })).toBe(
      3_000,
    )
    expect(interval({ state: { data: { agents: [makeAdminAgent()] } } })).toBe(10_000)
    expect(interval({ state: {} })).toBe(10_000)
  })

  it('sets a short stale time for the app detail', () => {
    expect(appQueryOptions('app_1').staleTime).toBe(2_000)
    expect(appsQueryOptions.staleTime).toBe(3_000)
    expect(meQueryOptions.staleTime).toBe(10_000)
  })
})

describe('query functions', () => {
  it('fetch through the api client', async () => {
    const http = mockApi({
      'GET /api/portal/me': ok({ user: {}, devices: [] }),
      'GET /api/portal/services': ok({ mode: 'managed' }),
      'GET /api/portal/apps': ok({ apps: [] }),
      'GET /api/portal/screens': ok({ screens: [] }),
      'GET /api/portal/apps/app_1': ok({ app: makeAppDetail() }),
      'GET /api/admin/overview': ok({ users: {} }),
      'GET /api/admin/settings': ok({ settings: {} }),
    })

    await meQueryOptions.queryFn!({} as never)
    await servicesQueryOptions.queryFn!({} as never)
    await appsQueryOptions.queryFn!({} as never)
    await screensQueryOptions.queryFn!({} as never)
    await appQueryOptions('app_1').queryFn!({} as never)
    await adminOverviewQueryOptions.queryFn!({} as never)
    await adminSettingsQueryOptions.queryFn!({} as never)

    expect(http.calls.map((call) => call.path)).toEqual([
      '/api/portal/me',
      '/api/portal/services',
      '/api/portal/apps',
      '/api/portal/screens',
      '/api/portal/apps/app_1',
      '/api/admin/overview',
      '/api/admin/settings',
    ])
  })
})
