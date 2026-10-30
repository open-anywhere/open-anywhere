import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { fail, mockApi, ok } from '../test/http'
import { makeServices, makeUser } from '../test/fixtures'
import { renderApp } from '../test/router'

function routes(overrides: Record<string, unknown> = {}) {
  return {
    'GET /api/portal/me': ok({ user: makeUser(), devices: [] }),
    'GET /api/portal/apps': ok({ apps: [] }),
    'GET /api/portal/screens': ok({ screens: [] }),
    'GET /api/portal/services': ok(makeServices()),
    ...overrides,
  }
}

describe('router', () => {
  it('shows a not-found page for an unknown address', async () => {
    mockApi(routes())
    await renderApp('/no-such-page')

    expect(await screen.findByText('页面不存在')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '回到我的空间' })).toHaveAttribute('href', '/portal')
  })

  it('sends the old console addresses to the portal', async () => {
    for (const path of ['/console', '/console/apps', '/portal/console', '/portal/console/apps']) {
      mockApi(routes())
      const { router, unmount } = await renderApp(path)
      expect(router.state.location.pathname).toBe('/portal')
      unmount()
    }
  })

  it('sends a signed-out visitor to login with the original address', async () => {
    mockApi(routes({ 'GET /api/portal/me': fail(401, 'UNAUTHORIZED', '请先登录') }))
    const { router } = await renderApp('/portal/apps')

    expect(router.state.location.pathname).toBe('/login')
    expect(router.state.location.search).toMatchObject({ redirect: '/portal/apps' })
  })

  it('sends a signed-out visitor from the admin area to login', async () => {
    mockApi(routes({ 'GET /api/portal/me': fail(401, 'UNAUTHORIZED', '请先登录') }))
    const { router } = await renderApp('/admin/users')

    expect(router.state.location.pathname).toBe('/login')
  })

  it('keeps members out of the admin area', async () => {
    mockApi(routes())
    const { router } = await renderApp('/admin/settings')

    expect(router.state.location.pathname).toBe('/portal')
  })
})
