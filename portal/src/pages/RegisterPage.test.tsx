import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { fail, mockApi, ok } from '../test/http'
import { makeDevice, makeServices, makeUser } from '../test/fixtures'
import { renderApp, settledAt } from '../test/router'

function routes(overrides: Record<string, unknown> = {}) {
  let meCalls = 0
  return {
    'GET /api/portal/me': () =>
      meCalls++ === 0
        ? fail(401, 'UNAUTHORIZED', '请先登录')
        : ok({ user: makeUser(), devices: [] }),
    'POST /api/portal/register': ok({ user: makeUser(), device: makeDevice() }),
    'GET /api/portal/apps': ok({ apps: [] }),
    'GET /api/portal/screens': ok({ screens: [] }),
    'GET /api/portal/services': ok(makeServices()),
    ...overrides,
  }
}

async function fill(username: string, password: string, confirm = password) {
  const user = userEvent.setup()
  await user.type(screen.getByLabelText('用户名'), username)
  await user.type(screen.getByLabelText('密码'), password)
  await user.type(screen.getByLabelText('确认密码'), confirm)
  return user
}

describe('RegisterPage', () => {
  it('asks for the password twice consistently', async () => {
    const http = mockApi(routes())
    await renderApp('/register')

    const user = await fill('ada', 'secret123', 'secret124')
    await user.click(screen.getByRole('button', { name: '创建账号，开始体验' }))

    expect(await screen.findByText('两次输入的密码不一致')).toBeInTheDocument()
    expect(http.callsFor('POST', '/api/portal/register')).toHaveLength(0)
  })

  it('creates the account and opens the new space', async () => {
    const http = mockApi(routes())
    const { router } = await renderApp('/register')

    const user = await fill('ada', 'secret123')
    await user.click(screen.getByRole('button', { name: '创建账号，开始体验' }))

    await settledAt(router, '/portal')
    const posts = http.callsFor('POST', '/api/portal/register')
    expect(posts).toHaveLength(1)
    expect(posts[0].body).toEqual({ username: 'ada', password: 'secret123' })
  })

  it('shows the gateway reason when registration is refused', async () => {
    mockApi(
      routes({
        'POST /api/portal/register': fail(403, 'REGISTRATION_CLOSED', '注册暂未开放'),
      }),
    )
    await renderApp('/register')

    const user = await fill('ada', 'secret123')
    await user.click(screen.getByRole('button', { name: '创建账号，开始体验' }))

    expect(await screen.findByText('注册暂未开放')).toBeInTheDocument()
  })

  it('falls back to a generic message for a network failure', async () => {
    mockApi(
      routes({
        'POST /api/portal/register': () => {
          throw new Error('offline')
        },
      }),
    )
    await renderApp('/register')

    const user = await fill('ada', 'secret123')
    await user.click(screen.getByRole('button', { name: '创建账号，开始体验' }))

    expect(await screen.findByText('注册失败，请稍后重试')).toBeInTheDocument()
  })

  it('sends a signed-in visitor to their space instead of the form', async () => {
    mockApi(
      routes({
        'GET /api/portal/me': ok({ user: makeUser(), devices: [] }),
      }),
    )
    const { router } = await renderApp('/register')

    await settledAt(router, '/portal')
    expect(screen.queryByText('创建账号')).toBeNull()
  })
})
