import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { mockApi, ok } from '../../test/http'
import { makeAgentOptions, makeAgentSettings, makeApp, makeDevice, makeUser } from '../../test/fixtures'
import { renderApp } from '../../test/router'

function routes(overrides: Record<string, unknown> = {}) {
  return {
    'GET /api/portal/me': ok({ user: makeUser(), devices: [makeDevice()] }),
    'GET /api/portal/apps': ok({ apps: [makeApp()] }),
    'GET /api/portal/devices/dev_1/agent': ok({ agent: makeAgentSettings() }),
    'GET /api/portal/agent-options': ok(makeAgentOptions()),
    'GET /api/portal/devices/dev_1/mcps': ok({ required: null, available: [] }),
    ...overrides,
  }
}

describe('AgentsPage', () => {
  it('shows the agent, its devices and the apps it built', async () => {
    mockApi(routes())
    await renderApp('/portal/agents')

    expect(await screen.findByRole('heading', { name: '客厅' })).toBeInTheDocument()
    expect(screen.getByText(/这个智能体下有 1 台设备/)).toBeInTheDocument()
    expect(await screen.findByText('它为这些场合做过的应用')).toBeInTheDocument()
    expect(screen.getAllByText('电影知识竞赛').length).toBeGreaterThan(0)
    expect(screen.getAllByRole('link', { name: '全部应用' })[0]).toHaveAttribute(
      'href',
      '/portal/apps',
    )
  })

  it('offers to create the first agent', async () => {
    const user = userEvent.setup()
    const http = mockApi(
      routes({
        'GET /api/portal/me': ok({ user: makeUser(), devices: [] }),
        'GET /api/portal/apps': ok({ apps: [] }),
        'POST /api/portal/devices': ok({ device: makeDevice({ state: 'provisioning' }) }),
      }),
    )
    await renderApp('/portal/agents')

    expect(await screen.findByText('还没有自己的智能体')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /准备好我的智能体/ }))

    await waitFor(() =>
      expect(http.callsFor('POST', '/api/portal/devices')).toHaveLength(1),
    )
    expect(http.callsFor('POST', '/api/portal/devices')[0].body).toEqual({})
  })

  it('opens the agent settings from the panel', async () => {
    const user = userEvent.setup()
    mockApi(routes())
    await renderApp('/portal/agents')

    await user.click(await screen.findByRole('button', { name: /智能体设置/ }))

    expect(await screen.findByLabelText('助手名称')).toHaveValue('小灵')
  })

  it('groups hardware under the agent it joined', async () => {
    mockApi(
      routes({
        'GET /api/portal/me': ok({
          user: makeUser(),
          devices: [
            makeDevice(),
            makeDevice({
              id: 'dev_2',
              kind: 'physical',
              name: '客厅遥控器',
              mac: 'AA:BB:CC:DD:EE:02',
              voice: { state: 'offline', connected: false },
            }),
          ],
        }),
        'GET /api/portal/apps': ok({ apps: [] }),
      }),
    )
    await renderApp('/portal/agents')

    const note = await screen.findByText(/这个智能体下有 2 台设备/)
    expect(note).toBeInTheDocument()
    expect(screen.getByText('客厅遥控器')).toBeInTheDocument()
    // One panel for the agent, not one per device.
    expect(screen.getAllByRole('heading', { name: '客厅' })).toHaveLength(1)
    expect(document.querySelectorAll('.device-panel')).toHaveLength(1)
  })
})
