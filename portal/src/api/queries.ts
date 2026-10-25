import { queryOptions } from '@tanstack/react-query'
import { adminApi, api } from './index'

export const meQueryOptions = queryOptions({
  queryKey: ['me'] as const,
  queryFn: api.me,
  staleTime: 10_000,
  retry: false,
})

export const servicesQueryOptions = queryOptions({
  queryKey: ['services'] as const,
  queryFn: api.services,
  staleTime: 10_000,
  retry: false,
})

export const appsQueryOptions = queryOptions({
  queryKey: ['apps'] as const,
  queryFn: api.listApps,
  staleTime: 3_000,
  retry: false,
})

export function appQueryOptions(appId: string) {
  return queryOptions({
    queryKey: ['app', appId] as const,
    queryFn: () => api.getApp(appId),
    staleTime: 2_000,
    retry: false,
  })
}

export const screensQueryOptions = queryOptions({
  queryKey: ['screens'] as const,
  queryFn: api.listScreens,
  staleTime: 5_000,
  refetchInterval: 10_000,
  retry: false,
})

export const adminOverviewQueryOptions = queryOptions({
  queryKey: ['admin', 'overview'] as const,
  queryFn: adminApi.overview,
  staleTime: 5_000,
  refetchInterval: 10_000,
  retry: false,
})

export function adminUsersQueryOptions(params: {
  query?: string
  status?: string
  limit?: number
  offset?: number
}) {
  return queryOptions({
    queryKey: ['admin', 'users', params] as const,
    queryFn: () => adminApi.users(params),
    staleTime: 5_000,
    refetchInterval: 10_000,
    retry: false,
  })
}

export function adminUserQueryOptions(userId: string) {
  return queryOptions({
    queryKey: ['admin', 'user', userId] as const,
    queryFn: () => adminApi.user(userId),
    staleTime: 5_000,
    refetchInterval: (query) =>
      query.state.data?.user.agents.some((agent) => agent.state === 'provisioning')
        ? 2_000
        : 10_000,
    retry: false,
  })
}

export function adminAgentsQueryOptions(params: { state?: string; query?: string }) {
  return queryOptions({
    queryKey: ['admin', 'agents', params] as const,
    queryFn: () => adminApi.agents(params),
    staleTime: 5_000,
    refetchInterval: (query) =>
      query.state.data?.agents.some((agent) => agent.state === 'provisioning') ? 3_000 : 10_000,
    retry: false,
  })
}

export function adminAppsQueryOptions(params: {
  state?: string
  query?: string
  limit?: number
}) {
  return queryOptions({
    queryKey: ['admin', 'apps', params] as const,
    queryFn: () => adminApi.apps(params),
    staleTime: 5_000,
    refetchInterval: 10_000,
    retry: false,
  })
}

export const adminSettingsQueryOptions = queryOptions({
  queryKey: ['admin', 'settings'] as const,
  queryFn: adminApi.settings,
  staleTime: 10_000,
  refetchInterval: 10_000,
  retry: false,
})
