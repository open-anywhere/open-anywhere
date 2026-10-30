import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import type { User } from '../api'
import { fail, mockApi, ok } from '../test/http'
import { makeServices, makeUser } from '../test/fixtures'
import { renderApp, settledAt } from '../test/router'

/** The first probe is the signed-out one; after login the session has a user. */
function meAfterLogin(user: User) {
  let calls = 0
  return () =>
    calls++ === 0
      ? fail(401, 'UNAUTHORIZED', '请先登录')
      : ok({ user, devices: [] })
}

function routes(user: User, overrides: Record<string, unknown> = {}) {
  return {
    'GET /api/portal/me': meAfterLogin(user),
    'POST /api/portal/login': ok({ user }),
    'GET /api/portal/apps': ok({ apps: [] }),
    'GET /api/portal/screens': ok({ screens: [] }),
    'GET /api/portal/services': ok(makeServices()),
    'GET /api/admin/overview': ok({
      users: { total: 1, active: 1, disabled: 0, admins: 1 },
      agents: { total: 0, registered: 0, provisioning: 0, error: 0, disabled: 0 },
      apps: { total: 0, running: 0, building: 0, error: 0, idle: 0 },
      platform: {
        agentapi: true,
        linx: true,
        mcp: true,
        services: true,
        registration_open: false,
      },
    }),
    ...overrides,
  }
}

async function login(username = 'ada', password = 'secret123') {
  const user = userEvent.setup()
  await user.type(screen.getByLabelText('用户名'), username)
  await user.type(screen.getByLabelText('密码'), password)
  await user.click(screen.getByRole('button', { name: /登\s*录/ }))
  return user
}

describe('LoginPage', () => {
  it('validates the form before sending it', async () => {
    const user = userEvent.setup()
    const http = mockApi(routes(makeUser()))
    await renderApp('/login')

    await user.click(screen.getByRole('button', { name: /登\s*录/ }))

    expect(await screen.findByText('请输入用户名')).toBeInTheDocument()
    expect(screen.getByText('请输入密码')).toBeInTheDocument()
    expect(http.callsFor('POST', '/api/portal/login')).toHaveLength(0)
  })

  it('rejects a short password', async () => {
    mockApi(routes(makeUser()))
    await renderApp('/login')
    await login('ada', 'short')

    expect(await screen.findByText('密码长度需在 8–128 个字符之间')).toBeInTheDocument()
  })

  it('signs a member in and opens their portal', async () => {
    const http = mockApi(routes(makeUser()))
    const { router } = await renderApp('/login')

    await login('ada', 'secret123')

    await settledAt(router, '/portal')
    const posts = http.callsFor('POST', '/api/portal/login')
    expect(posts).toHaveLength(1)
    expect(posts[0].body).toEqual({ username: 'ada', password: 'secret123' })
    expect(await screen.findByText('你好，ada')).toBeInTheDocument()
  })

  it('takes an administrator to the console', async () => {
    mockApi(routes(makeUser({ role: 'admin' })))
    const { router } = await renderApp('/login')

    await login()

    await settledAt(router, '/admin')
  })

  it('honours a safe redirect', async () => {
    mockApi(routes(makeUser()))
    const { router } = await renderApp('/login?redirect=%2Fportal%2Fsettings')

    await login()

    await settledAt(router, '/portal/settings')
  })

  it('ignores a redirect to another origin', async () => {
    mockApi(routes(makeUser()))
    const { router } = await renderApp('/login?redirect=%2F%2Fevil.example')

    await login()

    await settledAt(router, '/portal')
  })

  it('shows why a login failed', async () => {
    mockApi(
      routes(makeUser(), {
        'POST /api/portal/login': fail(401, 'INVALID_CREDENTIALS', '用户名或密码不正确'),
      }),
    )
    const { router } = await renderApp('/login')

    await login()

    expect(await screen.findByText('用户名或密码不正确')).toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/login')
  })

  it('falls back to a generic message for a network failure', async () => {
    mockApi(
      routes(makeUser(), {
        'POST /api/portal/login': () => {
          throw new Error('offline')
        },
      }),
    )
    await renderApp('/login')

    await login()

    expect(await screen.findByText('登录失败，请稍后重试')).toBeInTheDocument()
  })

  it('lets a signed-in visitor switch accounts', async () => {
    mockApi(
      routes(makeUser(), {
        'GET /api/portal/me': ok({ user: makeUser(), devices: [] }),
      }),
    )
    const { router } = await renderApp('/login?switchAccount=1')

    expect(await screen.findByText(/当前登录的是 ada/)).toBeInTheDocument()
    // Switching stays on the form instead of bouncing to the portal.
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
  })

  it('keeps an already signed-in visitor out of the form', async () => {
    let calls = 0
    mockApi(
      routes(makeUser(), {
        'GET /api/portal/me': () => {
          calls += 1
          return ok({ user: makeUser(), devices: [] })
        },
      }),
    )
    const { router } = await renderApp('/login')

    await settledAt(router, '/portal')
    expect(calls).toBeGreaterThan(0)
  })
})
