import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { fail, mockApi, ok } from '../../test/http'
import { makeDevice, makeServices, makeUser } from '../../test/fixtures'
import { renderApp, settledAt } from '../../test/router'

function portalRoutes(overrides: Record<string, unknown> = {}) {
  return {
    'GET /api/portal/me': ok({ user: makeUser(), devices: [makeDevice()] }),
    'GET /api/portal/apps': ok({ apps: [] }),
    'GET /api/portal/services': ok(makeServices()),
    ...overrides,
  }
}

describe('ConsoleLayout', () => {
  it('shows the portal navigation and the signed-in account', async () => {
    mockApi(portalRoutes())
    await renderApp('/portal')

    const nav = within(screen.getByRole('navigation', { name: '控制台导航' }))
    for (const label of ['概览', '智能体', '设备', '应用', '服务', '动态', '设置']) {
      expect(nav.getByRole('link', { name: label })).toBeInTheDocument()
    }
    expect(nav.getByRole('link', { name: '概览' })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByText('ada')).toBeInTheDocument()
    expect(screen.getByText('成员')).toBeInTheDocument()
    // The non-admin sidebar has no labelled admin group.
    expect(screen.queryByText('管理')).toBeNull()
    expect(screen.queryByRole('link', { name: '用户' })).toBeNull()
  })

  it('adds the admin group for administrators', async () => {
    mockApi(
      portalRoutes({
        'GET /api/portal/me': ok({ user: makeUser({ role: 'admin' }), devices: [] }),
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
      }),
    )
    await renderApp('/admin')

    expect(screen.getByText('管理')).toBeInTheDocument()
    expect(screen.getByText('管理员')).toBeInTheDocument()
    const nav = within(screen.getByRole('navigation', { name: '控制台导航' }))
    for (const label of ['管理概览', '用户', '全部智能体', '全部应用', '平台设置']) {
      expect(nav.getByRole('link', { name: label })).toBeInTheDocument()
    }
    // The top bar names the current page.
    expect(screen.getByText('管理概览', { selector: '.portal-topbar-title' })).toBeInTheDocument()
  })

  it('opens the drawer and navigates from it', async () => {
    const user = userEvent.setup()
    mockApi(portalRoutes())
    const { router } = await renderApp('/portal')

    await user.click(screen.getByRole('button', { name: '打开导航' }))
    const links = await screen.findAllByRole('link', { name: '设置' })
    expect(links).toHaveLength(2)
    expect(document.querySelector('.ant-drawer-open')).not.toBeNull()

    await user.click(links[1])
    await settledAt(router, '/portal/settings')
    await waitFor(() => expect(document.querySelector('.ant-drawer-open')).toBeNull())
  })

  it('logs out from the account menu', async () => {
    const user = userEvent.setup()
    const http = mockApi(portalRoutes({ 'POST /api/portal/logout': ok(null) }))
    const { router } = await renderApp('/portal')

    await user.click(screen.getByRole('button', { name: '账号菜单' }))
    await user.click(await screen.findByText('退出登录'))

    await waitFor(() => expect(http.callsFor('POST', '/api/portal/logout')).toHaveLength(1))
    await settledAt(router, '/')
  })

  it('sends a signed-out visitor to the login page', async () => {
    mockApi(portalRoutes({ 'GET /api/portal/me': fail(401, 'UNAUTHORIZED', '请先登录') }))
    const { router } = await renderApp('/portal')

    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
    expect(decodeURIComponent(router.state.location.href)).toContain('redirect=/portal')
    expect(await screen.findByText('欢迎回来')).toBeInTheDocument()
  })
})
