import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { fail, mockApi, ok } from '../../test/http'
import { makeAdminOverview, makeUser } from '../../test/fixtures'
import { renderApp } from '../../test/router'

function routes(overrides: Record<string, unknown> = {}) {
  return {
    'GET /api/portal/me': ok({ user: makeUser({ role: 'admin' }), devices: [] }),
    'GET /api/admin/overview': ok(makeAdminOverview()),
    ...overrides,
  }
}

describe('AdminOverviewPage', () => {
  it('shows platform counts and upstream status', async () => {
    mockApi(routes())
    await renderApp('/admin')

    expect(await screen.findByText('12')).toBeInTheDocument()
    const grid = document.querySelector('.stat-grid') as HTMLElement
    expect(within(grid).getByText('12')).toBeInTheDocument()
    expect(within(grid).getByText('10 个启用 · 2 个停用')).toBeInTheDocument()
    expect(within(grid).getByText('10')).toBeInTheDocument()
    expect(within(grid).getByText('8 个已注册 · 1 个开通中')).toBeInTheDocument()
    expect(within(grid).getByText('7')).toBeInTheDocument()
    expect(within(grid).getByText('2 个运行中 · 1 个构建中')).toBeInTheDocument()
    expect(within(grid).getByText('关闭')).toBeInTheDocument()
    expect(screen.getAllByText('已配置')).toHaveLength(4)
    expect(screen.getByText(/管理员：/)).toBeInTheDocument()
    expect(screen.getByText(/开通失败：/)).toBeInTheDocument()
    expect(screen.queryByText(/还有上游服务未配置/)).toBeNull()
  })

  it('guides the administrator through missing upstream services', async () => {
    mockApi(
      routes({
        'GET /api/admin/overview': ok(
          makeAdminOverview({
            platform: {
              agentapi: true,
              linx: false,
              mcp: false,
              services: false,
              registration_open: false,
            },
          }),
        ),
      }),
    )
    await renderApp('/admin')

    expect(
      await screen.findByText('还有上游服务未配置：Linx、灵矽 MCP 地址、默认屏幕服务'),
    ).toBeInTheDocument()
    expect(screen.getByText('配置向导')).toBeInTheDocument()
    expect(screen.getByText('请备份主密钥')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: '去配置' }).length).toBe(2)
  })

  it('reports a failed overview', async () => {
    mockApi(routes({ 'GET /api/admin/overview': fail(500, 'FAILED', '数据库不可用') }))
    await renderApp('/admin')

    expect(await screen.findByText('无法加载平台概览')).toBeInTheDocument()
    expect(screen.getByText('数据库不可用')).toBeInTheDocument()
  })

  it('reloads on demand', async () => {
    const user = userEvent.setup()
    const http = mockApi(routes())
    await renderApp('/admin')

    await screen.findByText('平台概览')
    await user.click(screen.getByRole('button', { name: /刷\s*新/ }))

    await waitFor(() =>
      expect(http.callsFor('GET', '/api/admin/overview').length).toBeGreaterThanOrEqual(2),
    )
  })

  it('marks registration as open', async () => {
    mockApi(
      routes({
        'GET /api/admin/overview': ok(
          makeAdminOverview({
            platform: {
              agentapi: true,
              linx: true,
              mcp: true,
              services: true,
              registration_open: true,
            },
          }),
        ),
      }),
    )
    await renderApp('/admin')

    expect(await screen.findByText('开放')).toBeInTheDocument()
    expect(screen.getByText('任何人都可以注册')).toBeInTheDocument()
  })
})
