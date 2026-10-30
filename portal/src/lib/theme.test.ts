import { describe, expect, it, vi } from 'vitest'
import {
  applyDocumentTheme,
  readThemeMode,
  resolveTheme,
  systemTheme,
  writeThemeMode,
} from './theme'
import { setSystemPrefersDark } from '../test/setup'

describe('readThemeMode', () => {
  it('defaults to system', () => {
    expect(readThemeMode()).toBe('system')
  })

  it('returns a stored mode', () => {
    window.localStorage.setItem('oa-theme', 'dark')
    expect(readThemeMode()).toBe('dark')
    window.localStorage.setItem('oa-theme', 'light')
    expect(readThemeMode()).toBe('light')
  })

  it('ignores unknown stored values', () => {
    window.localStorage.setItem('oa-theme', 'sepia')
    expect(readThemeMode()).toBe('system')
  })

  it('survives an unavailable localStorage', () => {
    const spy = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked')
    })
    expect(readThemeMode()).toBe('system')
    spy.mockRestore()
  })
})

describe('writeThemeMode', () => {
  it('stores the mode', () => {
    writeThemeMode('light')
    expect(window.localStorage.getItem('oa-theme')).toBe('light')
  })

  it('does not throw when storage is blocked', () => {
    const spy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked')
    })
    expect(() => writeThemeMode('dark')).not.toThrow()
    spy.mockRestore()
  })
})

describe('systemTheme', () => {
  it('reads the system preference', () => {
    expect(systemTheme()).toBe('light')
    setSystemPrefersDark(true)
    expect(systemTheme()).toBe('dark')
  })
})

describe('resolveTheme', () => {
  it('follows the system in system mode', () => {
    expect(resolveTheme('system', 'dark')).toBe('dark')
    expect(resolveTheme('system', 'light')).toBe('light')
  })

  it('keeps an explicit mode', () => {
    expect(resolveTheme('light', 'dark')).toBe('light')
    expect(resolveTheme('dark', 'light')).toBe('dark')
  })
})

describe('applyDocumentTheme', () => {
  it('writes the theme onto the document', () => {
    applyDocumentTheme('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(document.documentElement.style.colorScheme).toBe('dark')

    applyDocumentTheme('light')
    expect(document.documentElement.dataset.theme).toBe('light')
  })
})
