import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { AgentSettingsModal } from './AgentSettingsModal'
import { fail, mockApi, ok } from '../../../test/http'
import { makeAgentOptions, makeAgentSettings, makeDevice } from '../../../test/fixtures'
import { renderInRouter } from '../../../test/router'

function routes(overrides: Record<string, unknown> = {}) {
  return {
    'GET /api/portal/devices/dev_1/agent': ok({ agent: makeAgentSettings() }),
    'GET /api/portal/agent-options': ok(makeAgentOptions()),
    'GET /api/portal/devices/dev_1/mcps': ok({ required: null, available: [] }),
    'PUT /api/portal/devices/dev_1/agent': ok({ agent: makeAgentSettings() }),
    'POST /api/portal/devices/dev_1/reconnect': ok({ device: makeDevice() }),
    ...overrides,
  }
}

async function setup(overrides: Partial<Parameters<typeof AgentSettingsModal>[0]> = {}, api = routes()) {
  const onClose = vi.fn()
  const http = mockApi(api)
  await renderInRouter(<AgentSettingsModal deviceId="dev_1" onClose={onClose} {...overrides} />)
  // The drawer is open once the saved settings have arrived.
  await screen.findByDisplayValue('小灵')
  return { onClose, http }
}

describe('AgentSettingsModal', () => {
  it('renders nothing without a device', async () => {
    mockApi(routes())
    await renderInRouter(<AgentSettingsModal onClose={vi.fn()} />)
    expect(screen.queryByText('智能体设置')).toBeNull()
  })

  it('fills the form from the platform', async () => {
    await setup()

    expect(screen.getByLabelText('助手名称')).toHaveValue('小灵')
    expect(screen.getByText('身份与语言')).toBeInTheDocument()
    expect(screen.getByText('声音')).toBeInTheDocument()
    expect(screen.getByText('记忆与隐私')).toBeInTheDocument()
    expect(screen.getByText('MCP 服务')).toBeInTheDocument()
    expect(screen.getByText('晴川（女声 · 温柔）')).toBeInTheDocument()
    expect(screen.getByText('长期记忆')).toBeInTheDocument()
  })

  it('shows a spinner until both requests finish', async () => {
    mockApi(routes())
    await renderInRouter(<AgentSettingsModal deviceId="dev_1" onClose={vi.fn()} />)
    expect(document.querySelector('.ant-spin')).toBeInTheDocument()
  })

  it('explains a failed load', async () => {
    mockApi(routes({ 'GET /api/portal/devices/dev_1/agent': fail(502, 'UNAVAILABLE', '平台没有响应') }))
    await renderInRouter(<AgentSettingsModal deviceId="dev_1" onClose={vi.fn()} />)

    expect(await screen.findByText('暂时无法读取智能体设置')).toBeInTheDocument()
    expect(screen.getByText('平台没有响应')).toBeInTheDocument()
  })

  it('closes without saving when nothing changed', async () => {
    const user = userEvent.setup()
    const { onClose, http } = await setup()

    await user.click(screen.getByRole('button', { name: /保\s*存/ }))
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1))
    expect(http.callsFor('PUT', '/api/portal/devices/dev_1/agent')).toHaveLength(0)
  })

  it('sends only the field the user changed', async () => {
    const user = userEvent.setup()
    const { http, onClose } = await setup()

    const name = screen.getByLabelText('助手名称')
    await user.clear(name)
    await user.type(name, '小助手')
    await user.click(screen.getByRole('button', { name: /保\s*存/ }))

    await waitFor(() => expect(onClose).toHaveBeenCalled())
    const put = http.callsFor('PUT', '/api/portal/devices/dev_1/agent')
    expect(put).toHaveLength(1)
    expect(put[0].body).toEqual({ assistant_name: '小助手' })
  })

  it('refuses to save an empty name', async () => {
    const user = userEvent.setup()
    const { http, onClose } = await setup()

    await user.clear(screen.getByLabelText('助手名称'))
    await user.click(screen.getByRole('button', { name: /保\s*存/ }))

    expect(await screen.findByText('请填写助手名称')).toBeInTheDocument()
    expect(http.callsFor('PUT', '/api/portal/devices/dev_1/agent')).toHaveLength(0)
    expect(onClose).not.toHaveBeenCalled()
  })

  it('restores the platform default prompt', async () => {
    const user = userEvent.setup()
    const { http } = await setup()

    await user.click(screen.getByRole('button', { name: '恢复平台默认' }))
    await user.click(screen.getByRole('button', { name: /保\s*存/ }))

    await waitFor(() =>
      expect(http.callsFor('PUT', '/api/portal/devices/dev_1/agent')[0]?.body).toEqual({
        system_prompt: '默认角色介绍',
      }),
    )
  })

  it('turns the greeting off through the inverted flag', async () => {
    const user = userEvent.setup()
    const { http } = await setup()

    await user.click(screen.getByRole('switch'))
    await user.click(screen.getByRole('button', { name: /保\s*存/ }))

    await waitFor(() =>
      expect(http.callsFor('PUT', '/api/portal/devices/dev_1/agent')[0]?.body).toEqual({
        disable_greeting: true,
      }),
    )
  })

  it('keeps a memory mode the portal does not offer', async () => {
    await setup(
      {},
      routes({
        'GET /api/portal/devices/dev_1/agent': ok({
          agent: makeAgentSettings({ mem_model_id: 'memory_custom' }),
        }),
      }),
    )

    expect(screen.getByText('memory_custom（当前）')).toBeInTheDocument()
  })

  it('offers to reconnect after saving on a browser agent', async () => {
    const user = userEvent.setup()
    const { http } = await setup({ reconnectable: true })

    const name = screen.getByLabelText('助手名称')
    await user.clear(name)
    await user.type(name, '小助手')
    await user.click(screen.getByRole('button', { name: /保\s*存/ }))

    expect(await screen.findByText('重新连接后，新设置才会生效。')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '现在重新连接' }))
    await waitFor(() =>
      expect(http.callsFor('POST', '/api/portal/devices/dev_1/reconnect')).toHaveLength(1),
    )
    expect(await screen.findByText('已重新连接')).toBeInTheDocument()
  })

  it('tells hardware owners to restart the device', async () => {
    const user = userEvent.setup()
    await setup()

    await user.clear(screen.getByLabelText('助手名称'))
    await user.type(screen.getByLabelText('助手名称'), '小助手')
    await user.click(screen.getByRole('button', { name: /保\s*存/ }))

    expect(
      await screen.findByText('智能体设置已保存。硬件设备需要重启，新设置才会生效'),
    ).toBeInTheDocument()
  })

  it('shows the gateway message when saving fails', async () => {
    const user = userEvent.setup()
    await setup(
      {},
      routes({
        'PUT /api/portal/devices/dev_1/agent': fail(400, 'INVALID', '名称里有不允许的字符'),
      }),
    )

    await user.clear(screen.getByLabelText('助手名称'))
    await user.type(screen.getByLabelText('助手名称'), '小助手')
    await user.click(screen.getByRole('button', { name: /保\s*存/ }))

    expect(await screen.findByText('名称里有不允许的字符')).toBeInTheDocument()
  })

  it('falls back to a generic message for a non-API failure', async () => {
    const user = userEvent.setup()
    await setup(
      {},
      routes({
        'PUT /api/portal/devices/dev_1/agent': () => {
          throw new Error('socket closed')
        },
      }),
    )

    await user.clear(screen.getByLabelText('助手名称'))
    await user.type(screen.getByLabelText('助手名称'), '小助手')
    await user.click(screen.getByRole('button', { name: /保\s*存/ }))

    expect(await screen.findByText('保存失败')).toBeInTheDocument()
  })

  it('plays a demo for a voice that has one', async () => {
    await setup(
      {},
      routes({
        'GET /api/portal/agent-options': ok(
          makeAgentOptions({
            voices: [
              {
                id: 'voice_1',
                name: '晴川',
                gender: 'female',
                languages: ['zh-CN'],
                demo: 'https://audio.example.com/qingchuan.mp3',
              },
            ],
          }),
        ),
      }),
    )

    const audio = document.querySelector('audio')
    expect(audio).toHaveAttribute('src', 'https://audio.example.com/qingchuan.mp3')
    expect(audio).toHaveAttribute('controls')
  })

  it('hides voices that cannot speak the chosen language', async () => {
    await setup(
      {},
      routes({
        'GET /api/portal/agent-options': ok(
          makeAgentOptions({
            voices: [
              { id: 'voice_1', name: '晴川', languages: ['zh-CN'] },
              { id: 'voice_en', name: 'Helen', languages: ['en-US'] },
            ],
          }),
        ),
      }),
    )

    expect(screen.getByText('晴川')).toBeInTheDocument()
    await userEvent.setup().click(screen.getByLabelText('音色'))
    await waitFor(() =>
      expect(document.querySelectorAll('.ant-select-item-option').length).toBeGreaterThan(0),
    )
    expect(screen.queryByText('Helen')).toBeNull()
  })

  it('closes from the footer cancel button', async () => {
    const user = userEvent.setup()
    const { onClose } = await setup()

    await user.click(screen.getByRole('button', { name: /取\s*消/ }))
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
