import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { fail, mockApi, ok } from '../test/http'
import { makeDevice, makeUser } from '../test/fixtures'
import { renderApp, settledAt } from '../test/router'

const signedOut = { 'GET /api/portal/me': fail(401, 'UNAUTHORIZED', '请先登录') }

describe('HomePage', () => {
  it('describes the product to a visitor', async () => {
    mockApi(signedOut)
    await renderApp('/')

    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('说出你想玩的')
    expect(screen.getByText('为此刻的现场，造一个应用')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '怎么开始' })).toHaveAttribute('href', '#how')
    expect(screen.getByRole('link', { name: '适合的现场' })).toHaveAttribute('href', '#scenes')
    expect(screen.getByRole('link', { name: '登录' })).toHaveAttribute('href', '/login')
    expect(screen.getByRole('link', { name: '免费开始' })).toHaveAttribute('href', '/register')

    // Three steps, six scenes and three differences.
    expect(screen.getByText('说出此刻的需要')).toBeInTheDocument()
    expect(screen.getByText('应用当场出现')).toBeInTheDocument()
    expect(screen.getByText('抬头一起参与')).toBeInTheDocument()
    expect(screen.getByText('朋友聚会')).toBeInTheDocument()
    expect(screen.getByText('教室与培训')).toBeInTheDocument()
    expect(screen.getByText('每一场都不一样')).toBeInTheDocument()
    expect(screen.getAllByRole('article')).toHaveLength(12)
  })

  it('sends a visitor to registration', async () => {
    const user = userEvent.setup()
    mockApi(signedOut)
    const { router } = await renderApp('/')

    await user.click(screen.getAllByRole('button', { name: '免费开始' })[0])
    await settledAt(router, '/register')
  })

  it('points a signed-in visitor at their space', async () => {
    const user = userEvent.setup()
    mockApi({
      'GET /api/portal/me': ok({ user: makeUser(), devices: [makeDevice()] }),
      'GET /api/portal/apps': ok({ apps: [] }),
      'GET /api/portal/screens': ok({ screens: [] }),
      'GET /api/portal/services': ok({
        mode: 'managed',
        ready: true,
        managed: { available: true, render_url: '', casting_url: '' },
        custom: {
          render_url: '',
          casting_url: '',
          render_token_configured: false,
          casting_token_configured: false,
        },
      }),
    })
    const { router } = await renderApp('/')

    expect(await screen.findByRole('link', { name: '我的空间' })).toHaveAttribute(
      'href',
      '/portal',
    )
    expect(screen.queryByRole('link', { name: '免费开始' })).toBeNull()
    expect(screen.getAllByRole('button', { name: '进入我的空间' }).length).toBeGreaterThan(0)

    await user.click(screen.getAllByRole('button', { name: '切换账号' })[0])
    await waitFor(() =>
      expect(decodeURIComponent(router.state.location.href)).toContain('/login?switchAccount=true'),
    )
  })
})
