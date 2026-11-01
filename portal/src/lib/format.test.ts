import { describe, expect, it } from 'vitest'
import {
  appStateLabels,
  deviceStateLabels,
  formatDate,
  formatDuration,
  formatRelative,
  stepLabels,
  voiceStateLabels,
} from './format'

describe('formatDate', () => {
  it('shows a dash for missing or invalid values', () => {
    expect(formatDate()).toBe('—')
    expect(formatDate('')).toBe('—')
    expect(formatDate('not-a-date')).toBe('—')
  })

  it('formats a timestamp in local time', () => {
    const formatted = formatDate('2026-10-02T10:30:00Z')
    expect(formatted).not.toBe('—')
    expect(formatted).toMatch(/10/)
    expect(formatted).toMatch(/\d{2}:\d{2}/)
  })
})

describe('formatRelative', () => {
  const now = Date.now()

  it('falls back to the absolute date when the value is missing or invalid', () => {
    expect(formatRelative()).toBe('—')
    expect(formatRelative('nope')).toBe('—')
  })

  it('describes the last minute as 刚刚', () => {
    expect(formatRelative(new Date(now - 5_000).toISOString())).toBe('刚刚')
  })

  it('describes minutes and hours', () => {
    expect(formatRelative(new Date(now - 5 * 60_000).toISOString())).toBe('5 分钟前')
    expect(formatRelative(new Date(now - 2 * 3_600_000).toISOString())).toBe('2 小时前')
  })

  it('describes the previous day starting with 昨天', () => {
    expect(formatRelative(new Date(now - 30 * 3_600_000).toISOString())).toMatch(/^昨天 /)
  })

  it('uses the absolute date for anything older', () => {
    const value = new Date(now - 10 * 86_400_000).toISOString()
    expect(formatRelative(value)).toBe(formatDate(value))
  })

  it('uses the absolute date for the future', () => {
    const value = new Date(now + 3_600_000).toISOString()
    expect(formatRelative(value)).toBe(formatDate(value))
  })
})

describe('formatDuration', () => {
  it('shows a dash for missing, zero or negative durations', () => {
    expect(formatDuration()).toBe('—')
    expect(formatDuration(0)).toBe('—')
    expect(formatDuration(-4)).toBe('—')
  })

  it('rounds seconds', () => {
    expect(formatDuration(12)).toBe('12 秒')
    expect(formatDuration(12.6)).toBe('13 秒')
  })

  it('splits minutes and seconds', () => {
    expect(formatDuration(90)).toBe('1 分 30 秒')
    expect(formatDuration(120)).toBe('2 分钟')
  })

  it('splits hours and minutes', () => {
    expect(formatDuration(3_600)).toBe('1 小时 0 分')
    expect(formatDuration(5_400)).toBe('1 小时 30 分')
  })
})

describe('label maps', () => {
  it('cover every state the API can return', () => {
    expect(stepLabels.create_agent).toBe('创建智能体')
    expect(appStateLabels.building).toBe('生成中')
    expect(appStateLabels.running).toBe('屏幕上')
    expect(deviceStateLabels.registered).toBe('已准备好')
    expect(voiceStateLabels.listening).toBe('收听中')
  })
})
