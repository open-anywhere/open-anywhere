import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { fail, mockApi, ok } from '../../test/http'
import { makeAdminApp, makeUser } from '../../test/fixtures'
import { renderApp } from '../../test/router'

function appsPage(apps = [makeAdminApp()]) {
  return ok({ apps, limit: 200 })
}

function routes(overrides: Record<string, unknown> = {}) {
  return {
    'GET /api/portal/me': ok({ user: makeUser({ role: 'admin' }), devices: [] }),
    'GET /api/admin/apps': appsPage(),
    ...overrides,
  }
}

async function openApps() {
  const result = await renderApp('/admin/apps')
  await screen.findByText('电影知识竞赛')
  return result
}

describe('AdminAppsPage', () => {
  it('lists every app with its owner and state', async () => {
    mockApi(routes())
    await openApps()

    const row = screen.getByText('电影知识竞赛').closest('tr') as HTMLElement
    expect(within(row).getByText('app_1')).toBeInTheDocument()
    expect(within(row).getByText('agent_1')).toBeInTheDocument()
    expect(within(row).getByText('屏幕上')).toBeInTheDocument()
    expect(within(row).getByText('2')).toBeInTheDocument()
    expect(within(row).getByRole('link', { name: 'ada' })).toHaveAttribute(
      'href',
      '/admin/users/u_1',
    )
    expect(screen.getByText('1 个应用')).toBeInTheDocument()
  })

  it('falls back for an app with no owner or agent', async () => {
    mockApi(
      routes({
        'GET /api/admin/apps': appsPage([
          makeAdminApp({
            title: '',
            name: '',
            user_id: '',
            username: '',
            agent_id: '',
            state: 'building',
          }),
        ]),
      }),
    )
    await renderApp('/admin/apps')

    expect(await screen.findByText('未命名应用')).toBeInTheDocument()
    expect(screen.getByText('未知用户')).toBeInTheDocument()
    expect(screen.getByText('生成中')).toBeInTheDocument()
    expect(screen.getByText('—')).toBeInTheDocument()
  })

  it('searches apps, users and agents', async () => {
    const user = userEvent.setup()
    const http = mockApi(routes())
    await openApps()

    await user.type(screen.getByPlaceholderText('搜索应用、用户或智能体'), '电影{Enter}')

    await waitFor(() =>
      expect(http.callsFor('GET', '/api/admin/apps').at(-1)?.url.searchParams.get('query')).toBe(
        '电影',
      ),
    )
  })

  it('filters by app state', async () => {
    const user = userEvent.setup()
    const http = mockApi(routes())
    await openApps()

    await user.click(screen.getByRole('combobox', { name: '按状态筛选' }))
    const dropdown = document.querySelector('.ant-select-dropdown') as HTMLElement
    await waitFor(() => expect(within(dropdown).getByText('构建中')).toBeInTheDocument())
    await user.click(within(dropdown).getByText('构建中'))

    await waitFor(() =>
      expect(http.callsFor('GET', '/api/admin/apps').at(-1)?.url.searchParams.get('state')).toBe(
        'building',
      ),
    )
  })

  it('reports a failed list', async () => {
    mockApi(routes({ 'GET /api/admin/apps': fail(500, 'FAILED', '应用表不可用') }))
    await renderApp('/admin/apps')

    expect(await screen.findByText('无法加载应用列表')).toBeInTheDocument()
    expect(screen.getByText('应用表不可用')).toBeInTheDocument()
  })
})
