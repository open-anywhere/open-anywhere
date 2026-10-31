import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { fail, mockApi, ok } from '../../test/http'
import { makeMcp, makePlatformSettings, makeUser } from '../../test/fixtures'
import { renderApp } from '../../test/router'

function settingsPage(overrides: Record<string, unknown> = {}) {
  return {
    'GET /api/portal/me': ok({ user: makeUser({ role: 'admin' }), devices: [] }),
    'GET /api/admin/settings': ok({ settings: makePlatformSettings() }),
    'GET /api/admin/mcp': ok({
      provisioned: true,
      services: [
        makeMcp(),
        makeMcp({ id: 'mcp_2', name: '网关', desc: '控制屏幕', required: true, shared: true }),
      ],
    }),
    'PUT /api/admin/settings': ok({ settings: makePlatformSettings() }),
    'POST /api/admin/settings/test': ok({ linx: { ok: true }, render: { ok: true } }),
    'PUT /api/admin/mcp/shared': ok({ ids: ['mcp_1'] }),
    'POST /api/admin/mcp/sync': ok({ synced: true }),
    ...overrides,
  }
}

async function openSettings() {
  const result = await renderApp('/admin/settings')
  await screen.findByDisplayValue('https://agent.qiniuapi.com')
  return result
}

describe('AdminSettingsPage', () => {
  it('shows the platform settings and what is ready', async () => {
    mockApi(settingsPage())
    await openSettings()

    expect(screen.getByDisplayValue('https://agent.qiniuapi.com')).toBeInTheDocument()
    expect(screen.getByDisplayValue('agt_1')).toBeInTheDocument()
    expect(screen.getByDisplayValue('env_1')).toBeInTheDocument()
    expect(screen.getByDisplayValue('https://xrobo.qiniu.com')).toBeInTheDocument()
    expect(screen.getByDisplayValue('https://xrobo.qiniuapi.com/v1/ota/')).toBeInTheDocument()
    expect(screen.getByDisplayValue('https://gateway.example.com/mcp')).toBeInTheDocument()
    expect(screen.getByDisplayValue('小灵')).toBeInTheDocument()
    expect(screen.getByDisplayValue('https://render.example.com')).toBeInTheDocument()
    // Every group is configured, so the status tags all read 已配置.
    expect(screen.getAllByText('已配置').length).toBeGreaterThanOrEqual(4)
  })

  it('saves only the group that changed', async () => {
    const user = userEvent.setup()
    const http = mockApi(settingsPage())
    await openSettings()

    const base = screen.getByDisplayValue('https://agent.qiniuapi.com')
    await user.clear(base)
    await user.type(base, 'https://agent.example.com')
    await user.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(() => expect(http.callsFor('PUT', '/api/admin/settings')).toHaveLength(1))
    expect(http.callsFor('PUT', '/api/admin/settings')[0].body).toEqual({
      agentapi: { base_url: 'https://agent.example.com' },
    })
    expect(await screen.findByText('平台设置已保存，新构建和新开通会立即使用新值')).toBeInTheDocument()
  })

  it('sends an empty patch when nothing changed', async () => {
    const user = userEvent.setup()
    const http = mockApi(settingsPage())
    await openSettings()

    await user.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(() => expect(http.callsFor('PUT', '/api/admin/settings')).toHaveLength(1))
    expect(http.callsFor('PUT', '/api/admin/settings')[0].body).toEqual({})
  })

  it('saves the registration switch', async () => {
    const user = userEvent.setup()
    const http = mockApi(settingsPage())
    await openSettings()

    // Direct DOM lookups keep this page's expensive role queries out of the way.
    await user.click(document.querySelectorAll('.ant-tabs-tab')[4] as HTMLElement)
    await user.click(document.querySelector('.ant-switch') as HTMLElement)
    await user.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(() => expect(http.callsFor('PUT', '/api/admin/settings')).toHaveLength(1))
    expect(http.callsFor('PUT', '/api/admin/settings')[0].body).toEqual({
      registration: { open: true },
    })
  })

  it('tests the upstream connections without saving', async () => {
    const user = userEvent.setup()
    const http = mockApi(settingsPage())
    await openSettings()

    await user.click(screen.getByRole('button', { name: /测试连接/ }))

    await waitFor(() => expect(http.callsFor('POST', '/api/admin/settings/test')).toHaveLength(1))
    expect(http.callsFor('POST', '/api/admin/settings/test')[0].body).toEqual({})
    expect(http.callsFor('PUT', '/api/admin/settings')).toHaveLength(0)
  })

  it('warns when part of the test fails', async () => {
    const user = userEvent.setup()
    mockApi(
      settingsPage({
        'POST /api/admin/settings/test': ok({
          linx: { ok: false, message: 'API Key 无效' },
          render: { ok: true },
          casting: { ok: false, message: '地址不可达' },
          screens: 0,
        }),
      }),
    )
    await openSettings()

    await user.click(screen.getByRole('button', { name: /测试连接/ }))

    expect(await screen.findByText(/Linx：API Key 无效/)).toBeInTheDocument()
    expect(screen.getByText(/Render：/)).toBeInTheDocument()
  })

  it('shows the warning returned after saving', async () => {
    const user = userEvent.setup()
    mockApi(
      settingsPage({
        'PUT /api/admin/settings': ok({
          settings: makePlatformSettings(),
          warning: 'MCP 资源缓存已清空，下一次通话会重新加载',
        }),
      }),
    )
    await openSettings()

    const base = screen.getByDisplayValue('https://agent.qiniuapi.com')
    await user.clear(base)
    await user.type(base, 'https://agent.example.com')
    await user.click(screen.getByRole('button', { name: '保存设置' }))

    expect(
      await screen.findByText('MCP 资源缓存已清空，下一次通话会重新加载'),
    ).toBeInTheDocument()
  })

  it('reports a failed save', async () => {
    const user = userEvent.setup()
    mockApi(
      settingsPage({
        'PUT /api/admin/settings': fail(409, 'BUSY', '还有应用占用默认屏幕服务'),
      }),
    )
    await openSettings()

    await user.click(screen.getByRole('button', { name: '保存设置' }))

    expect(await screen.findByText('还有应用占用默认屏幕服务')).toBeInTheDocument()
  })

  it('reports settings that cannot be read', async () => {
    mockApi(
      settingsPage({ 'GET /api/admin/settings': fail(503, 'UNAVAILABLE', '数据库不可用') }),
    )
    await renderApp('/admin/settings')

    expect(await screen.findByText('无法读取平台设置')).toBeInTheDocument()
    expect(screen.getByText('数据库不可用')).toBeInTheDocument()
  })

  it('lists the Linx MCP services and shares the optional ones', async () => {
    const user = userEvent.setup()
    const http = mockApi(settingsPage())
    await openSettings()

    await user.click(screen.getByRole('tab', { name: 'MCP 服务' }))
    expect(await screen.findByText('天气')).toBeInTheDocument()
    expect(screen.getByText('查天气')).toBeInTheDocument()
    expect(screen.getByText('始终启用')).toBeInTheDocument()
    await user.click(screen.getByRole('checkbox', { name: '开放给用户' }))

    await waitFor(() => expect(http.callsFor('PUT', '/api/admin/mcp/shared')).toHaveLength(1))
    expect(http.callsFor('PUT', '/api/admin/mcp/shared')[0].body).toEqual({
      ids: ['mcp_2', 'mcp_1'],
    })
  })

  it('syncs the MCP address and token', async () => {
    const user = userEvent.setup()
    const http = mockApi(settingsPage())
    await openSettings()

    await user.click(screen.getByRole('tab', { name: 'MCP 服务' }))
    await user.click(await screen.findByRole('button', { name: /同步地址与 Token/ }))

    await waitFor(() => expect(http.callsFor('POST', '/api/admin/mcp/sync')).toHaveLength(1))
    expect(
      await screen.findByText('已把当前地址和 Token 同步到灵矽的 MCP 服务'),
    ).toBeInTheDocument()
  })

  it('explains an unprovisioned MCP service and an empty list', async () => {
    mockApi(settingsPage({ 'GET /api/admin/mcp': ok({ provisioned: false, services: [] }) }))
    await openSettings()

    expect(await screen.findByText('网关自己的 MCP 服务还没有创建')).toBeInTheDocument()
    expect(screen.getByText('灵矽账号下还没有 MCP 服务')).toBeInTheDocument()
  })

  it('explains when the MCP services cannot be read', async () => {
    mockApi(settingsPage({ 'GET /api/admin/mcp': fail(400, 'NO_KEY', '请先配置 API Key') }))
    await openSettings()

    expect(await screen.findByText('暂时无法读取灵矽的 MCP 服务')).toBeInTheDocument()
    expect(screen.getByText('请先配置 API Key')).toBeInTheDocument()
    expect(within(document.body).queryByText('天气')).toBeNull()
  })
})
