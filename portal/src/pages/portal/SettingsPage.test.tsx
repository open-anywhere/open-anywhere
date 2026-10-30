import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { fail, mockApi, ok } from '../../test/http'
import { makeDevice, makeServices, makeUser } from '../../test/fixtures'
import { renderApp, settledAt } from '../../test/router'

function routes(overrides: Record<string, unknown> = {}) {
  return {
    'GET /api/portal/me': ok({ user: makeUser(), devices: [makeDevice()] }),
    'GET /api/portal/services': ok(makeServices()),
    'POST /api/portal/logout': ok(null),
    ...overrides,
  }
}

describe('SettingsPage', () => {
  it('shows the account and the screen connection', async () => {
    mockApi(routes())
    await renderApp('/portal/settings')

    expect((await screen.findAllByText('ada')).length).toBeGreaterThan(0)
    expect(screen.getByText('普通用户')).toBeInTheDocument()
    expect(await screen.findByText('平台提供')).toBeInTheDocument()
    expect(await screen.findByText('已就绪')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /管理连接/ })).toHaveAttribute(
      'href',
      '/portal/services',
    )
  })

  it('marks administrators', async () => {
    mockApi(routes({ 'GET /api/portal/me': ok({ user: makeUser({ role: 'admin' }), devices: [] }) }))
    await renderApp('/portal/settings')

    expect(await screen.findByText('管理员', { selector: '.ant-tag' })).toBeInTheDocument()
  })

  it('applies and remembers the chosen theme', async () => {
    const user = userEvent.setup()
    mockApi(routes())
    await renderApp('/portal/settings')

    await screen.findAllByText('ada')
    await user.click(screen.getByText('深色'))

    await waitFor(() => expect(document.documentElement.dataset.theme).toBe('dark'))
    expect(window.localStorage.getItem('oa-theme')).toBe('dark')
    expect(document.documentElement.style.colorScheme).toBe('dark')
  })

  it('says when the screen connection cannot be read', async () => {
    mockApi(routes({ 'GET /api/portal/services': fail(502, 'UNAVAILABLE', '网关没有响应') }))
    await renderApp('/portal/settings')

    expect(await screen.findByText('暂时无法读取屏幕连接。')).toBeInTheDocument()
  })

  it('logs out and returns home', async () => {
    const user = userEvent.setup()
    const http = mockApi(routes())
    const { router } = await renderApp('/portal/settings')

    await user.click(await screen.findByRole('button', { name: /退出登录/ }))

    await waitFor(() => expect(http.callsFor('POST', '/api/portal/logout')).toHaveLength(1))
    await settledAt(router, '/')
  })
})
