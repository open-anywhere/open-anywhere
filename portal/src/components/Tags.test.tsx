import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type { AppState, ScreenStatus } from '../api'
import { AppStateTag, DeviceStateTag, ScreenStatusTag, VoiceTag } from './Tags'

function pill(): HTMLElement {
  const element = document.querySelector('.pill')
  if (!element) throw new Error('no pill rendered')
  return element as HTMLElement
}

describe('AppStateTag', () => {
  it.each([
    ['building', '生成中', 'warning'],
    ['idle', '待播放', 'neutral'],
    ['running', '屏幕上', 'success'],
    ['error', '未完成', 'danger'],
  ] as const)('renders %s with the %s label', (state, label, tone) => {
    render(<AppStateTag state={state} />)
    expect(screen.getByText(label)).toBeInTheDocument()
    expect(pill()).toHaveClass(`pill-${tone}`)
  })

  it('falls back to neutral for an unknown state', () => {
    render(<AppStateTag state={'paused' as AppState} />)
    expect(screen.getByText('paused')).toBeInTheDocument()
    expect(pill()).toHaveClass('pill-neutral')
  })
})

describe('VoiceTag', () => {
  it('shows success whenever the voice connection is up', () => {
    render(<VoiceTag state="listening" connected />)
    expect(screen.getByText('收听中')).toHaveClass('pill-success')
  })

  it.each([
    ['error', 'danger'],
    ['offline', 'neutral'],
    ['disconnected', 'neutral'],
    ['reconnecting', 'warning'],
    ['thinking', 'primary'],
  ] as const)('maps a disconnected %s state to %s', (state, tone) => {
    render(<VoiceTag state={state} connected={false} />)
    expect(pill()).toHaveClass(`pill-${tone}`)
  })
})

describe('DeviceStateTag', () => {
  it.each([
    ['registered', '已准备好', 'success'],
    ['provisioning', '准备中', 'warning'],
    ['error', '需要处理', 'danger'],
    ['disabled', '已停用', 'neutral'],
  ] as const)('renders %s', (state, label, tone) => {
    render(<DeviceStateTag state={state} />)
    expect(screen.getByText(label)).toHaveClass(`pill-${tone}`)
  })
})

describe('ScreenStatusTag', () => {
  it('shows a neutral offline pill for an unavailable offline screen', () => {
    render(<ScreenStatusTag status="unavailable" online={false} />)
    expect(screen.getByText('离线')).toHaveClass('pill-neutral')
  })

  it('still explains an online screen', () => {
    render(<ScreenStatusTag status="unavailable" online />)
    expect(screen.getByText('不可用')).toHaveClass('pill-danger')
  })

  it.each([
    ['showing_app', '正在播放', 'success'],
    ['external', '被其他内容占用', 'warning'],
    ['free', '空闲', 'neutral'],
  ] as const)('renders %s', (status, label, tone) => {
    render(<ScreenStatusTag status={status as ScreenStatus} online />)
    expect(screen.getByText(label)).toHaveClass(`pill-${tone}`)
  })
})
