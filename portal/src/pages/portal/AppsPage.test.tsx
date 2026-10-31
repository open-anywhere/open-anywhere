import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { fail, mockApi, ok } from '../../test/http'
import { makeApp, makeDevice, makeUser } from '../../test/fixtures'
import { renderApp, settledAt } from '../../test/router'

const running = makeApp({ app_id: 'app_1', title: '电影知识竞赛', state: 'running' })
const idle = makeApp({ app_id: 'app_2', title: '旅行抢答', state: 'idle' })

function routes(overrides: Record<string, unknown> = {}) {
  return {
    'GET /api/portal/me': ok({ user: makeUser(), devices: [makeDevice()] }),
    'GET /api/portal/apps': ok({ apps: [running, idle] }),
    ...overrides,
  }
}

describe('AppsPage', () => {
  it('lists every app with its state', async () => {
    mockApi(routes())
    await renderApp('/portal/apps')

    expect(await screen.findByText('电影知识竞赛')).toBeInTheDocument()
    expect(screen.getByText('旅行抢答')).toBeInTheDocument()
    expect(screen.getByText('2 个应用')).toBeInTheDocument()
    expect(screen.getByText('屏幕上', { selector: '.pill' })).toBeInTheDocument()
    expect(screen.getByText('待播放', { selector: '.pill' })).toBeInTheDocument()
  })

  it('filters by a search term and clears it again', async () => {
    const user = userEvent.setup()
    mockApi(routes())
    await renderApp('/portal/apps')

    await user.type(await screen.findByPlaceholderText('搜索应用'), '旅行')
    expect(await screen.findByText('1 / 2 个应用')).toBeInTheDocument()
    expect(screen.queryByText('电影知识竞赛')).toBeNull()

    await user.clear(screen.getByPlaceholderText('搜索应用'))
    await user.type(screen.getByPlaceholderText('搜索应用'), '不存在的应用')
    expect(await screen.findByText('没有匹配的应用')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '清除筛选' }))
    expect(await screen.findByText('2 个应用')).toBeInTheDocument()
    expect(screen.getByText('电影知识竞赛')).toBeInTheDocument()
  })

  it('filters by state', async () => {
    const user = userEvent.setup()
    mockApi(routes())
    await renderApp('/portal/apps')

    await screen.findByText('电影知识竞赛')
    await user.click(screen.getByLabelText('按状态筛选'))
    await waitFor(() => expect(document.querySelector('.ant-select-dropdown')).not.toBeNull())
    const dropdown = within(document.querySelector('.ant-select-dropdown') as HTMLElement)
    await user.click(dropdown.getByText('屏幕上'))

    await waitFor(() => expect(screen.queryByText('旅行抢答')).toBeNull())
    expect(screen.getByText('电影知识竞赛')).toBeInTheDocument()
    expect(screen.getByText('1 / 2 个应用')).toBeInTheDocument()
  })

  it('filters by agent when the account has several', async () => {
    const user = userEvent.setup()
    mockApi(
      routes({
        'GET /api/portal/me': ok({
          user: makeUser(),
          devices: [
            makeDevice(),
            makeDevice({ id: 'dev_2', name: '书房', mac: 'AA:BB:CC:DD:EE:02', agent_id: 'agent_2', agent_name: '小书' }),
          ],
        }),
        'GET /api/portal/apps': ok({
          apps: [running, { ...idle, device_id: 'AA:BB:CC:DD:EE:02' }],
        }),
      }),
    )
    await renderApp('/portal/apps')

    await screen.findByText('电影知识竞赛')
    await user.click(screen.getByLabelText('按智能体筛选'))
    await user.click(await screen.findByText('小书'))

    await waitFor(() => expect(screen.queryByText('电影知识竞赛')).toBeNull())
    expect(screen.getByText('旅行抢答')).toBeInTheDocument()
  })

  it('invites the owner to talk to the agent when there are no apps', async () => {
    const user = userEvent.setup()
    mockApi(routes({ 'GET /api/portal/apps': ok({ apps: [] }) }))
    const { router } = await renderApp('/portal/apps')

    expect(await screen.findByText('还没有应用')).toBeInTheDocument()
    const buttons = await screen.findAllByRole('button', { name: /开始对话/ })
    await user.click(buttons[0])

    await settledAt(router, '/portal/call')
    expect(router.state.location.search).toEqual({ device: 'dev_1' })
  })

  it('explains why the list failed to load', async () => {
    mockApi(routes({ 'GET /api/portal/apps': fail(500, 'FAILED', '应用服务出错') }))
    await renderApp('/portal/apps')

    expect(await screen.findByText('无法加载应用列表')).toBeInTheDocument()
    expect(screen.getByText('应用服务出错')).toBeInTheDocument()
  })
})
