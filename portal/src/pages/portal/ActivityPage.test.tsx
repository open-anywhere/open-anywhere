import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { fail, mockApi, ok } from '../../test/http'
import { makeApp, makeDevice, makeUser } from '../../test/fixtures'
import { renderApp } from '../../test/router'

const now = new Date()
const minutesAgo = (minutes: number) => new Date(now.getTime() - minutes * 60_000).toISOString()

function routes(overrides: Record<string, unknown> = {}) {
  return {
    'GET /api/portal/me': ok({
      user: makeUser(),
      devices: [
        makeDevice({
          created_at: minutesAgo(120),
          registered_at: minutesAgo(119),
        }),
      ],
    }),
    'GET /api/portal/apps': ok({
      apps: [
        makeApp({ state: 'idle', built_at: minutesAgo(60), build_started_at: minutesAgo(61) }),
        makeApp({
          app_id: 'app_2',
          title: '旅行抢答',
          state: 'idle',
          started_at: minutesAgo(2000),
        }),
      ],
    }),
    ...overrides,
  }
}

describe('ActivityPage', () => {
  it('groups the timeline by day', async () => {
    mockApi(routes())
    await renderApp('/portal/activity')

    expect(await screen.findByText('智能体已创建')).toBeInTheDocument()
    expect(screen.getByText('智能体准备好了')).toBeInTheDocument()
    expect(screen.getByText('「电影知识竞赛」生成完成')).toBeInTheDocument()
    expect(screen.getByText('「旅行抢答」开始播放')).toBeInTheDocument()

    expect(screen.getByText('今天')).toBeInTheDocument()
    expect(screen.getByText('昨天')).toBeInTheDocument()
  })

  it('shows work that is still running', async () => {
    mockApi(
      routes({
        'GET /api/portal/me': ok({
          user: makeUser(),
          devices: [makeDevice({ state: 'provisioning', step: 'create_agent', registered_at: undefined })],
        }),
        'GET /api/portal/apps': ok({
          apps: [
            makeApp({
              state: 'building',
              title: '旅行抢答',
              progress: '正在生成题目',
              build_started_at: minutesAgo(1),
            }),
          ],
        }),
      }),
    )
    await renderApp('/portal/activity')

    expect(await screen.findByText('进行中')).toBeInTheDocument()
    expect(screen.getByText(/正在准备 小灵/)).toBeInTheDocument()
    expect(await screen.findByText(/正在生成「旅行抢答」/)).toBeInTheDocument()
    expect(await screen.findByText('正在生成题目')).toBeInTheDocument()
  })

  it('says there is nothing yet', async () => {
    mockApi(
      routes({
        'GET /api/portal/me': ok({ user: makeUser(), devices: [] }),
        'GET /api/portal/apps': ok({ apps: [] }),
      }),
    )
    await renderApp('/portal/activity')

    expect(await screen.findByText('还没有动态')).toBeInTheDocument()
  })

  it('reports a failure', async () => {
    mockApi(routes({ 'GET /api/portal/apps': fail(500, 'FAILED', '应用服务出错') }))
    await renderApp('/portal/activity')

    expect(await screen.findByText('暂时无法加载动态')).toBeInTheDocument()
    expect(screen.getByText('应用服务出错')).toBeInTheDocument()
  })

  it('refreshes both sources on demand', async () => {
    const user = userEvent.setup()
    const http = mockApi(routes())
    await renderApp('/portal/activity')

    await screen.findByText('智能体已创建')
    await user.click(screen.getByRole('button', { name: /刷\s*新/ }))

    await waitFor(() => expect(http.callsFor('GET', '/api/portal/me')).toHaveLength(2))
    expect(http.callsFor('GET', '/api/portal/apps')).toHaveLength(2)
  })
})
