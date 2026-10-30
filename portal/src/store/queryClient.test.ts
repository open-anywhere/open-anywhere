import { describe, expect, it, vi } from 'vitest'

async function loadStore() {
  vi.resetModules()
  const [store, api] = await Promise.all([import('./queryClient'), import('../api')])
  return { ...store, ApiError: api.ApiError }
}

describe('authRedirectTarget', () => {
  it('sends an expired session on a portal page to login with a redirect', async () => {
    const { authRedirectTarget, ApiError } = await loadStore()
    expect(authRedirectTarget(new ApiError(401, 'UNAUTHORIZED', '过期'), '/portal/apps')).toBe(
      '/login?redirect=%2Fportal%2Fapps',
    )
  })

  it('keeps the query string in the redirect', async () => {
    const { authRedirectTarget, ApiError } = await loadStore()
    expect(
      authRedirectTarget(new ApiError(401, 'UNAUTHORIZED', '过期'), '/admin/users', '?status=active'),
    ).toBe('/login?redirect=%2Fadmin%2Fusers%3Fstatus%3Dactive')
  })

  it('treats /portal and /admin exactly as protected too', async () => {
    const { authRedirectTarget, ApiError } = await loadStore()
    expect(authRedirectTarget(new ApiError(401, 'UNAUTHORIZED', 'x'), '/portal')).toBeDefined()
    expect(authRedirectTarget(new ApiError(401, 'UNAUTHORIZED', 'x'), '/admin')).toBeDefined()
  })

  it('does not redirect a visitor who is not signed in on a public page', async () => {
    const { authRedirectTarget, ApiError } = await loadStore()
    expect(authRedirectTarget(new ApiError(401, 'UNAUTHORIZED', 'x'), '/')).toBeUndefined()
    expect(authRedirectTarget(new ApiError(401, 'UNAUTHORIZED', 'x'), '/login')).toBeUndefined()
    expect(authRedirectTarget(new ApiError(401, 'UNAUTHORIZED', 'x'), '/other')).toBeUndefined()
  })

  it('leaves login and register failures on the form', async () => {
    const { authRedirectTarget, ApiError } = await loadStore()
    expect(
      authRedirectTarget(new ApiError(401, 'INVALID', '密码不对', true), '/portal'),
    ).toBeUndefined()
  })

  it('sends a non-admin away from the admin area', async () => {
    const { authRedirectTarget, ApiError } = await loadStore()
    expect(authRedirectTarget(new ApiError(403, 'ADMIN_REQUIRED', 'x'), '/admin/users')).toBe('/portal')
    expect(authRedirectTarget(new ApiError(403, 'ADMIN_REQUIRED', 'x'), '/portal')).toBeUndefined()
  })

  it('ignores other failures', async () => {
    const { authRedirectTarget, ApiError } = await loadStore()
    expect(authRedirectTarget(new ApiError(403, 'OTHER', 'x'), '/admin/users')).toBeUndefined()
    expect(authRedirectTarget(new ApiError(500, 'SERVER', 'x'), '/portal')).toBeUndefined()
    expect(authRedirectTarget(new Error('boom'), '/portal')).toBeUndefined()
    expect(authRedirectTarget(undefined, '/portal')).toBeUndefined()
  })
})

describe('retry policy', () => {
  it('does not retry client errors', async () => {
    const store = await loadStore()
    const retry = store.queryClient.getDefaultOptions().queries?.retry
    const policy = retry as (count: number, error: unknown) => boolean
    expect(policy(0, new store.ApiError(400, 'BAD', 'x'))).toBe(false)
    expect(policy(0, new store.ApiError(500, 'SERVER', 'x'))).toBe(true)
    expect(policy(1, new store.ApiError(500, 'SERVER', 'x'))).toBe(false)
    expect(policy(0, new Error('network'))).toBe(true)
  })

  it('does not refetch when the window regains focus', async () => {
    const store = await loadStore()
    expect(store.queryClient.getDefaultOptions().queries?.refetchOnWindowFocus).toBe(false)
  })
})

describe('session failure handling', () => {
  it('clears cached tenant data and leaves the console on a 401', async () => {
    const store = await loadStore()
    // jsdom cannot navigate, so the redirect is only observed through the
    // cleared cache; it logs "Not implemented: navigation" instead.
    const jsdomError = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    window.history.pushState({}, '', '/portal/apps')

    await expect(
      store.queryClient.fetchQuery({
        queryKey: ['probe'],
        queryFn: () => {
          throw new store.ApiError(401, 'UNAUTHORIZED', '过期')
        },
        retry: false,
      }),
    ).rejects.toThrow('过期')

    expect(store.queryClient.getQueryCache().getAll()).toHaveLength(0)
    expect(window.location.pathname).toBe('/portal/apps')
    jsdomError.mockRestore()
    window.history.pushState({}, '', '/')
  })
})
