import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { fail, mockApi, ok } from '../../test/http'
import { makeApp, makeDevice, makeScreen, makeServices, makeUser } from '../../test/fixtures'
import { renderApp, settledAt } from '../../test/router'

function routes(overrides: Record<string, unknown> = {}) {
  return {
    'GET /api/portal/me': ok({ user: makeUser(), devices: [makeDevice()] }),
    'GET /api/portal/apps': ok({ apps: [makeApp({ state: 'running', screen_id: 'screen_1' })] }),
    'GET /api/portal/screens': ok({ screens: [makeScreen({ status: 'showing_app', current_app: { app_id: 'app_1', name: '电影知识竞赛' } })] }),
    'GET /api/portal/services': ok(makeServices()),
    ...overrides,
  }
}

describe('OverviewPage', () => {
  it('summarises agents, apps and screens', async () => {
    mockApi(routes())
    await renderApp('/portal')

    expect(await screen.findByText('你好，ada')).toBeInTheDocument()
    const stats = within(document.querySelector('.stat-grid') as HTMLElement)
    expect(stats.getByText('智能体')).toBeInTheDocument()
    expect(await screen.findByText('1 个已准备好')).toBeInTheDocument()
    expect(await screen.findByText('1 个正在播放 · 0 个生成中')).toBeInTheDocument()
    expect(await screen.findByText('1 个在线')).toBeInTheDocument()
    expect(screen.getByText('没有进行中的任务')).toBeInTheDocument()

    expect(screen.getAllByText('电影知识竞赛').length).toBeGreaterThan(0)
    expect(screen.getAllByText('客厅电视').length).toBeGreaterThan(0)
    expect(screen.getByText('已就绪')).toBeInTheDocument()
  })

  it('warns when casting is not ready and links to the settings', async () => {
    const user = userEvent.setup()
    mockApi(routes({ 'GET /api/portal/services': ok(makeServices({ ready: false })) }))
    const { router } = await renderApp('/portal')

    expect(await screen.findByText('投屏服务还没有准备好')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '检查连接' }))
    await settledAt(router, '/portal/services')
  })

  it('drives the first conversation from the header', async () => {
    const user = userEvent.setup()
    mockApi(routes())
    const { router } = await renderApp('/portal')

    await user.click((await screen.findAllByRole('button', { name: /开始对话/ }))[0])
    await waitFor(() =>
      expect(router.state.location.pathname).toBe('/portal/call'),
    )
    expect(router.state.location.search).toEqual({ device: 'dev_1' })
  })

  it('offers to create an agent when there is none', async () => {
    const user = userEvent.setup()
    const http = mockApi(
      routes({
        'GET /api/portal/me': ok({ user: makeUser(), devices: [] }),
        'GET /api/portal/apps': ok({ apps: [] }),
        'POST /api/portal/devices': ok({ device: makeDevice({ state: 'provisioning' }) }),
      }),
    )
    await renderApp('/portal')

    expect(await screen.findByText('先准备好你的智能体')).toBeInTheDocument()
    await user.click((await screen.findAllByRole('button', { name: /准备好我的智能体/ }))[0])

    await waitFor(() => expect(http.callsFor('POST', '/api/portal/devices')).toHaveLength(1))
  })

  it('reports a missing casting service instead of an empty screen list', async () => {
    mockApi(routes({ 'GET /api/portal/screens': fail(503, 'UNAVAILABLE', '投屏服务暂时不可用') }))
    await renderApp('/portal')

    expect(await screen.findByText('无法发现屏幕')).toBeInTheDocument()
    expect(screen.getAllByText('投屏服务暂时不可用').length).toBeGreaterThan(0)
    const stats = within(document.querySelector('.stat-grid') as HTMLElement)
    expect(stats.getByText('投屏服务暂时不可用')).toBeInTheDocument()
  })

  it('reports an app list failure', async () => {
    mockApi(routes({ 'GET /api/portal/apps': fail(500, 'FAILED', '应用服务出错') }))
    await renderApp('/portal')

    expect(await screen.findByText('无法加载应用')).toBeInTheDocument()
    expect(screen.getByText('应用服务出错')).toBeInTheDocument()
  })

  it('reports a workspace failure on the next refresh', async () => {
    const user = userEvent.setup()
    let meCalls = 0
    mockApi(
      routes({
        'GET /api/portal/me': () =>
          meCalls++ === 0
            ? ok({ user: makeUser(), devices: [makeDevice()] })
            : fail(500, 'FAILED', '数据库不可用'),
      }),
    )
    await renderApp('/portal')

    await user.click(await screen.findByRole('button', { name: /刷\s*新/ }))

    expect(await screen.findByText('暂时无法加载你的空间')).toBeInTheDocument()
    expect(screen.getByText('数据库不可用')).toBeInTheDocument()
  })
})
