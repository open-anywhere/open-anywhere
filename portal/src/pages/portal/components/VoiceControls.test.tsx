import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { VoiceControls } from './VoiceControls'

function setup(overrides: Partial<Parameters<typeof VoiceControls>[0]> = {}) {
  const handlers = {
    onStart: vi.fn(),
    onStop: vi.fn(),
    onToggleMicrophone: vi.fn(),
    onTogglePlayback: vi.fn(),
    onPause: vi.fn(),
    onResume: vi.fn(),
    onAbort: vi.fn(),
    onVolumeChange: vi.fn(),
  }
  const props = {
    supported: true,
    micActive: false,
    playbackActive: false,
    starting: false,
    paused: false,
    volume: 60,
    ...handlers,
    ...overrides,
  }
  const result = render(<VoiceControls {...props} />)
  return { ...result, ...handlers }
}

describe('VoiceControls before a call', () => {
  it('invites the user to speak and starts the call', async () => {
    const user = userEvent.setup()
    const { onStart } = setup()

    expect(screen.getByText('开口就好。浏览器会请求麦克风权限。')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /开始对话/ }))
    expect(onStart).toHaveBeenCalledTimes(1)
  })

  it('explains a browser without WebCodecs and disables the call', () => {
    setup({ supported: false })
    expect(
      screen.getByText('当前浏览器不支持语音对话，请使用最新版 Chrome 或 Edge 打开。'),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /开始对话/ })).toBeDisabled()
  })

  it('explains a disconnected agent and disables the call', () => {
    setup({ offline: true })
    expect(screen.getByText('智能体已断开连接，重新连接后才能对话。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /开始对话/ })).toBeDisabled()
  })

  it('shows a spinner while starting', () => {
    setup({ starting: true })
    expect(screen.getByRole('button', { name: /开始对话/ })).toHaveClass('ant-btn-loading')
  })
})

describe('VoiceControls during a call', () => {
  it('toggles the microphone and the playback', async () => {
    const user = userEvent.setup()
    const { onToggleMicrophone, onTogglePlayback } = setup({ micActive: true, playbackActive: true })

    await user.click(screen.getByRole('button', { name: /关闭麦克风/ }))
    expect(onToggleMicrophone).toHaveBeenCalledTimes(1)

    await user.click(screen.getByRole('button', { name: /关闭播放/ }))
    expect(onTogglePlayback).toHaveBeenCalledTimes(1)
  })

  it('offers to turn the microphone and the playback back on', async () => {
    const user = userEvent.setup()
    const { onToggleMicrophone, onTogglePlayback } = setup({ micActive: false, playbackActive: true })

    await user.click(screen.getByRole('button', { name: /开启麦克风/ }))
    await user.click(screen.getByRole('button', { name: /关闭播放/ }))
    expect(onToggleMicrophone).toHaveBeenCalledTimes(1)
    expect(onTogglePlayback).toHaveBeenCalledTimes(1)
  })

  it('pauses a running session', async () => {
    const user = userEvent.setup()
    const { onPause, onResume } = setup({ micActive: true })

    await user.click(screen.getByRole('button', { name: /暂停监听/ }))
    expect(onPause).toHaveBeenCalledTimes(1)
    expect(onResume).not.toHaveBeenCalled()
  })

  it('resumes a paused session', async () => {
    const user = userEvent.setup()
    const { onPause, onResume } = setup({ micActive: true, paused: true })

    await user.click(screen.getByRole('button', { name: /继续监听/ }))
    expect(onResume).toHaveBeenCalledTimes(1)
    expect(onPause).not.toHaveBeenCalled()
  })

  it('aborts the reply and ends the call', async () => {
    const user = userEvent.setup()
    const { onAbort, onStop } = setup({ playbackActive: true })

    await user.click(screen.getByRole('button', { name: /打断回复/ }))
    expect(onAbort).toHaveBeenCalledTimes(1)

    await user.click(screen.getByRole('button', { name: /结束对话/ }))
    expect(onStop).toHaveBeenCalledTimes(1)
  })

  it('shows the volume and reports a new one', async () => {
    const { onVolumeChange } = setup({ playbackActive: true, volume: 60 })

    expect(screen.getByText('60%')).toBeInTheDocument()
    const slider = screen.getByRole('slider')
    expect(slider).toHaveAttribute('aria-valuenow', '60')
    slider.focus()
    fireEvent.keyDown(slider, { key: 'ArrowRight', keyCode: 39, which: 39 })

    expect(onVolumeChange).toHaveBeenCalledWith(61)
  })
})
