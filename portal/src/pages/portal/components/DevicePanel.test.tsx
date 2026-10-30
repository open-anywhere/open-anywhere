import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { DevicePanel } from './DevicePanel'
import { makeDevice } from '../../../test/fixtures'
import { renderInRouter } from '../../../test/router'

async function setup(overrides: Partial<Parameters<typeof DevicePanel>[0]> = {}) {
  const handlers = {
    onCreate: vi.fn(),
    onRetry: vi.fn(),
    onCall: vi.fn(),
  }
  await renderInRouter(<DevicePanel {...handlers} {...overrides} />)
  return handlers
}

describe('DevicePanel without a device', () => {
  it('invites the user to create one', async () => {
    const user = userEvent.setup()
    const { onCreate } = await setup()

    expect(screen.getByText('还没有自己的智能体')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /准备好我的智能体/ }))
    expect(onCreate).toHaveBeenCalledTimes(1)
  })

  it('shows the create button busy while it runs', async () => {
    await setup({ creating: true })
    expect(screen.getByRole('button', { name: /准备好我的智能体/ })).toHaveClass(
      'ant-btn-loading',
    )
  })
})

describe('DevicePanel for a browser agent', () => {
  it('shows its identity and the actions a registered agent has', async () => {
    const user = userEvent.setup()
    const onReconnect = vi.fn()
    const onSettings = vi.fn()
    const handlers = await setup({
      device: makeDevice(),
      onReconnect,
      onDisconnect: vi.fn(),
      onSettings,
    })

    expect(screen.getByRole('heading', { name: '客厅' })).toBeInTheDocument()
    expect(screen.getByText('已准备好')).toBeInTheDocument()
    expect(screen.getByText('收听中')).toBeInTheDocument()
    expect(screen.getByText('随时可以开口，让它为你准备一个应用。')).toBeInTheDocument()
    expect(screen.getByText('小灵')).toBeInTheDocument()
    // A virtual device has no hardware id of its own.
    expect(screen.queryByText('设备 ID')).toBeNull()

    await user.click(screen.getByRole('button', { name: /开始对话/ }))
    expect(handlers.onCall).toHaveBeenCalledTimes(1)

    await user.click(screen.getByRole('button', { name: /重新连接/ }))
    expect(onReconnect).toHaveBeenCalledTimes(1)

    await user.click(screen.getByRole('button', { name: /智能体设置/ }))
    expect(onSettings).toHaveBeenCalledTimes(1)
  })

  it('falls back to the agent name and then a placeholder', async () => {
    await setup({ device: makeDevice({ name: undefined }) })
    expect(screen.getByRole('heading', { name: '小灵' })).toBeInTheDocument()

    await renderInRouter(
      <DevicePanel
        device={makeDevice({ name: undefined, agent_name: undefined })}
        onCreate={vi.fn()}
        onRetry={vi.fn()}
        onCall={vi.fn()}
      />,
    )
    expect(screen.getByRole('heading', { name: '你的智能体' })).toBeInTheDocument()
  })

  it('confirms before disconnecting', async () => {
    const user = userEvent.setup()
    const onDisconnect = vi.fn()
    await setup({ device: makeDevice(), onDisconnect })

    await user.click(screen.getByRole('button', { name: /断开连接/ }))
    expect(await screen.findByText('断开与灵矽的连接？')).toBeInTheDocument()
    expect(onDisconnect).not.toHaveBeenCalled()

    const buttons = await screen.findAllByRole('button', { name: /断\s*开/ })
    await user.click(buttons.at(-1) as HTMLElement)
    expect(onDisconnect).toHaveBeenCalledTimes(1)
  })

  it('warns when the agent is offline and offers to reconnect', async () => {
    const user = userEvent.setup()
    const onReconnect = vi.fn()
    await setup({
      device: makeDevice({ voice: { state: 'offline', connected: false } }),
      onReconnect,
    })

    expect(screen.getByText('智能体暂时不在线')).toBeInTheDocument()
    expect(screen.getByText('连接已断开或没有响应，可以重新连接。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /开始对话/ })).toBeDisabled()
    expect(screen.queryByRole('button', { name: /断开连接/ })).toBeNull()

    // The panel action and the header action share a label.
    const reconnect = screen.getAllByRole('button', { name: /重新连接/ })
    expect(reconnect).toHaveLength(2)
    await user.click(reconnect[0])
    expect(onReconnect).toHaveBeenCalledTimes(1)
  })

  it('shows the platform error for an offline agent', async () => {
    await setup({
      device: makeDevice({ voice: { state: 'error', connected: false, error: '超时未响应' } }),
      onReconnect: vi.fn(),
    })
    expect(screen.getByText('超时未响应')).toBeInTheDocument()
  })

  it('falls back to retry when there is nothing to reconnect', async () => {
    const user = userEvent.setup()
    const handlers = await setup({
      device: makeDevice({ voice: { state: 'offline', connected: false } }),
    })

    const reconnect = screen.getAllByRole('button', { name: /重新连接/ })
    await user.click(reconnect[0])
    expect(handlers.onRetry).toHaveBeenCalledTimes(1)
  })

  it('confirms before removing the device', async () => {
    const user = userEvent.setup()
    const onRemove = vi.fn()
    await setup({ device: makeDevice(), onRemove })

    await user.click(screen.getByRole('button', { name: /移除/ }))
    expect(await screen.findByText('移除这个智能体设备？')).toBeInTheDocument()

    const buttons = await screen.findAllByRole('button', { name: /移\s*除/ })
    await user.click(buttons.at(-1) as HTMLElement)
    expect(onRemove).toHaveBeenCalledTimes(1)
  })
})

describe('DevicePanel for hardware', () => {
  it('describes how a physical device works and hides voice controls', async () => {
    await setup({ device: makeDevice({ kind: 'physical', name: undefined, agent_name: undefined }) })

    expect(screen.getByRole('heading', { name: '硬件设备' })).toBeInTheDocument()
    expect(screen.getByText('硬件设备', { selector: '.ant-tag' })).toBeInTheDocument()
    expect(screen.queryByText('开始对话')).toBeNull()
    expect(screen.queryByText('断开连接')).toBeNull()
    expect(screen.getByText('设备自己连接灵矽，对它说话就能让应用出现在屏幕上。')).toBeInTheDocument()
    expect(screen.getByText('设备 ID')).toBeInTheDocument()
    expect(screen.getByText('AA:BB:CC:DD:EE:01')).toBeInTheDocument()
  })

  it('explains how to rebind a removed device', async () => {
    const user = userEvent.setup()
    await setup({ device: makeDevice({ kind: 'physical' }), onRemove: vi.fn() })

    await user.click(screen.getByRole('button', { name: /移除/ }))
    expect(await screen.findByText('移除这台硬件设备？')).toBeInTheDocument()
  })
})

describe('DevicePanel while provisioning', () => {
  it('shows the progress steps and a disabled retry', async () => {
    await setup({ device: makeDevice({ state: 'provisioning', step: 'ota' }) })

    expect(screen.getByText('准备智能体')).toBeInTheDocument()
    expect(
      screen.getByText('正在准备你的智能体，通常只需要一会儿。页面会自动刷新。'),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /重试/ })).toBeDisabled()
  })
})

describe('DevicePanel in an error state', () => {
  it('shows the failure and offers a retry', async () => {
    const user = userEvent.setup()
    const handlers = await setup({ device: makeDevice({ state: 'error', error: '激活码已使用' }) })

    expect(screen.getByText('准备过程中遇到了问题')).toBeInTheDocument()
    expect(screen.getByText('激活码已使用')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /重试/ }))
    expect(handlers.onRetry).toHaveBeenCalledTimes(1)
  })

  it('offers a generic reason when the platform gives none', async () => {
    await setup({ device: makeDevice({ state: 'error' }) })
    expect(screen.getByText('请稍后重试。')).toBeInTheDocument()
  })
})
