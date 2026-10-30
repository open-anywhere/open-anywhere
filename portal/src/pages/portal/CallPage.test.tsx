import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { fail, mockApi, ok } from '../../test/http'
import { makeDevice, makeUser } from '../../test/fixtures'
import { renderApp } from '../../test/router'

class FakeEventSource {
  static instances: FakeEventSource[] = []
  url: string
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  close = vi.fn()

  constructor(url: string) {
    this.url = url
    FakeEventSource.instances.push(this)
  }
}

function installEventSource() {
  FakeEventSource.instances = []
  vi.stubGlobal('EventSource', FakeEventSource)
}

function status(overrides: Record<string, unknown> = {}) {
  return {
    state: 'listening',
    connected: true,
    volume: 70,
    registered: true,
    ...overrides,
  }
}

function routes(devices = [makeDevice()], overrides: Record<string, unknown> = {}) {
  return {
    'GET /api/portal/me': ok({ user: makeUser(), devices }),
    'GET /voice/dev_1/status': ok(status()),
    'GET /api/portal/devices/dev_1/chat-history': ok({
      records: [
        { role: 'user', text: '来一场电影抢答', at: '2026-10-30T08:00:00Z' },
        { role: 'agent', text: '好的，第一题来了。', at: '2026-10-30T08:00:05Z' },
      ],
    }),
    ...overrides,
  }
}

describe('CallPage', () => {
  it('asks for an agent when the account has none', async () => {
    installEventSource()
    mockApi(routes([]))
    await renderApp('/portal/call')

    expect(await screen.findByText('还没有准备好你的智能体')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '去准备智能体' })).toHaveAttribute(
      'href',
      '/portal/agents',
    )
  })

  it('reports an agent that does not exist', async () => {
    installEventSource()
    mockApi(routes())
    await renderApp('/portal/call?device=missing')

    expect(await screen.findByText('找不到这个智能体')).toBeInTheDocument()
  })

  it('waits while the agent is still being provisioned', async () => {
    installEventSource()
    mockApi(routes([makeDevice({ state: 'provisioning', step: 'create_agent' })]))
    await renderApp('/portal/call?device=dev_1')

    expect(await screen.findByText('智能体还在准备中')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '查看准备进度' })).toHaveAttribute(
      'href',
      '/portal/agents',
    )
  })

  it('shows saved history and the live controls', async () => {
    installEventSource()
    mockApi(routes())
    await renderApp('/portal/call?device=dev_1')

    expect(await screen.findByText('来一场电影抢答')).toBeInTheDocument()
    expect(screen.getByText('好的，第一题来了。')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '小灵' })).toBeInTheDocument()
    expect(screen.getByText('已连接')).toBeInTheDocument()

    // jsdom has no WebCodecs, so the page explains the limitation.
    expect(await screen.findByText('当前浏览器不支持语音对话')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /开始对话/ })).toBeDisabled()
    expect(FakeEventSource.instances[0].url).toBe('/voice/dev_1/events')
  })

  it('starts on the empty transcript when there is no saved history', async () => {
    installEventSource()
    mockApi(routes([makeDevice()], { 'GET /api/portal/devices/dev_1/chat-history': ok({ records: [] }) }))
    await renderApp('/portal/call?device=dev_1')

    expect(await screen.findByText('等待对话开始')).toBeInTheDocument()
  })

  it('says when the agent is offline', async () => {
    installEventSource()
    mockApi(
      routes([
        makeDevice({ voice: { state: 'offline', connected: false } }),
      ], {
        'GET /voice/dev_1/status': ok(status({ state: 'offline', connected: false })),
      }),
    )
    await renderApp('/portal/call?device=dev_1')

    expect(await screen.findByText('智能体已断开连接')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /开始对话/ })).toBeDisabled()
    expect(screen.queryByRole('button', { name: /断开连接/ })).toBeNull()
  })

  it('reports a status probe failure', async () => {
    installEventSource()
    mockApi(routes([makeDevice()], {
      'GET /voice/dev_1/status': fail(503, 'UNAVAILABLE', '智能体服务不可用'),
    }))
    await renderApp('/portal/call?device=dev_1')

    await waitFor(() =>
      expect(screen.queryByText('智能体暂时没有响应')).not.toBeNull(),
    )
    expect(screen.getByText('智能体服务不可用')).toBeInTheDocument()
  })

  it('reconnects the agent on demand', async () => {
    installEventSource()
    const http = mockApi(routes([makeDevice()], {
      'POST /api/portal/devices/dev_1/reconnect': ok({ device: makeDevice() }),
    }))
    await renderApp('/portal/call?device=dev_1')

    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: /重新连接/ }))

    await waitFor(() =>
      expect(http.callsFor('POST', '/api/portal/devices/dev_1/reconnect')).toHaveLength(1),
    )
  })

  it('disconnects the agent after confirmation', async () => {
    installEventSource()
    const http = mockApi(routes([makeDevice()], {
      'POST /api/portal/devices/dev_1/disconnect': ok({ device: makeDevice() }),
    }))
    await renderApp('/portal/call?device=dev_1')

    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: /断开连接/ }))
    const confirms = await screen.findAllByRole('button', { name: '断 开' })
    await user.click(confirms.at(-1)!)

    await waitFor(() =>
      expect(http.callsFor('POST', '/api/portal/devices/dev_1/disconnect')).toHaveLength(1),
    )
  })
})
