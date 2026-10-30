import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { AgentMcpGroup } from './AgentMcpGroup'
import { fail, mockApi, ok, type MockResponse } from '../../../test/http'
import { makeMcp } from '../../../test/fixtures'
import { renderInRouter } from '../../../test/router'

function mcps(overrides: Record<string, unknown> = {}) {
  return ok({
    required: makeMcp({ id: 'mcp_core', name: '应用控制', desc: '控制屏幕上的应用', required: true }),
    available: [
      makeMcp({ id: 'mcp_weather', name: '天气', selected: false }),
      makeMcp({ id: 'mcp_music', name: '音乐', desc: '放点背景音乐', selected: true }),
    ],
    ...overrides,
  })
}

describe('AgentMcpGroup', () => {
  it('shows a skeleton while loading', async () => {
    let release: () => void = () => undefined
    const pending = new Promise<void>((resolve) => {
      release = resolve
    })
    mockApi({
      'GET /api/portal/devices/dev_1/mcps': async () => {
        await pending
        return mcps() as MockResponse
      },
    })
    await renderInRouter(<AgentMcpGroup deviceId="dev_1" />)
    expect(document.querySelector('.ant-skeleton')).toBeInTheDocument()

    release()
    expect(await screen.findByText('应用控制')).toBeInTheDocument()
    expect(document.querySelector('.ant-skeleton')).toBeNull()
  })

  it('lists the required and the optional services', async () => {
    const http = mockApi({ 'GET /api/portal/devices/dev_1/mcps': mcps() })
    await renderInRouter(<AgentMcpGroup deviceId="dev_1" />)

    expect(await screen.findByText('应用控制')).toBeInTheDocument()
    expect(screen.getByText('始终启用')).toBeInTheDocument()
    expect(screen.getByText('控制屏幕上的应用')).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: '应用控制' })).toBeChecked()
    expect(screen.getByRole('checkbox', { name: '应用控制' })).toBeDisabled()
    expect(screen.getByRole('checkbox', { name: '天气' })).not.toBeChecked()
    expect(screen.getByRole('checkbox', { name: '音乐' })).toBeChecked()
    expect(screen.getByText('放点背景音乐')).toBeInTheDocument()
    expect(http.callsFor('GET', '/api/portal/devices/dev_1/mcps')).toHaveLength(1)
  })

  it('explains a failed load', async () => {
    mockApi({ 'GET /api/portal/devices/dev_1/mcps': fail(500, 'SERVER', 'boom') })
    await renderInRouter(<AgentMcpGroup deviceId="dev_1" />)
    expect(await screen.findByText('暂时无法读取 MCP 服务。')).toBeInTheDocument()
  })

  it('says when nothing else is available', async () => {
    mockApi({ 'GET /api/portal/devices/dev_1/mcps': mcps({ required: null, available: [] }) })
    await renderInRouter(<AgentMcpGroup deviceId="dev_1" />)
    expect(await screen.findByText('管理员还没有开放其他 MCP 服务。')).toBeInTheDocument()
  })

  it('adds a service to the current selection', async () => {
    const user = userEvent.setup()
    const http = mockApi({
      'GET /api/portal/devices/dev_1/mcps': mcps(),
      'PUT /api/portal/devices/dev_1/mcps': ok({ ids: ['mcp_music', 'mcp_weather'] }),
    })
    await renderInRouter(<AgentMcpGroup deviceId="dev_1" />)

    await user.click(await screen.findByRole('checkbox', { name: '天气' }))
    expect(
      await screen.findByText('MCP 服务已更新。要让它生效，请重新连接智能体，硬件设备需要重启'),
    ).toBeInTheDocument()

    const put = http.callsFor('PUT', '/api/portal/devices/dev_1/mcps')
    expect(put).toHaveLength(1)
    expect(put[0].body).toEqual({ ids: ['mcp_music', 'mcp_weather'] })
  })

  it('removes a selected service', async () => {
    const user = userEvent.setup()
    const http = mockApi({
      'GET /api/portal/devices/dev_1/mcps': mcps(),
      'PUT /api/portal/devices/dev_1/mcps': ok({ ids: [] }),
    })
    await renderInRouter(<AgentMcpGroup deviceId="dev_1" />)

    await user.click(await screen.findByRole('checkbox', { name: '音乐' }))
    const put = http.callsFor('PUT', '/api/portal/devices/dev_1/mcps')
    expect(put[0].body).toEqual({ ids: [] })
  })

  it('shows the gateway message when saving fails', async () => {
    const user = userEvent.setup()
    mockApi({
      'GET /api/portal/devices/dev_1/mcps': mcps(),
      'PUT /api/portal/devices/dev_1/mcps': fail(409, 'BUSY', '智能体正在对话'),
    })
    await renderInRouter(<AgentMcpGroup deviceId="dev_1" />)

    await user.click(await screen.findByRole('checkbox', { name: '天气' }))
    expect(await screen.findByText('智能体正在对话')).toBeInTheDocument()
  })

  it('falls back to a generic message for a non-API failure', async () => {
    const user = userEvent.setup()
    mockApi({
      'GET /api/portal/devices/dev_1/mcps': mcps(),
      'PUT /api/portal/devices/dev_1/mcps': () => {
        throw new Error('socket closed')
      },
    })
    await renderInRouter(<AgentMcpGroup deviceId="dev_1" />)

    await user.click(await screen.findByRole('checkbox', { name: '天气' }))
    expect(await screen.findByText('保存失败')).toBeInTheDocument()
  })
})
