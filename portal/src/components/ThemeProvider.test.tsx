import { act, render, renderHook, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { ThemeProvider, useThemeMode } from './ThemeProvider'
import { ThemeToggle } from './ThemeToggle'
import { setSystemPrefersDark } from '../test/setup'

function Probe() {
  const { mode, resolved } = useThemeMode()
  return (
    <span data-testid="mode">
      {mode}:{resolved}
    </span>
  )
}

describe('ThemeProvider', () => {
  it('follows the system by default', async () => {
    render(
      <ThemeProvider>
        <Probe />
      </ThemeProvider>,
    )

    expect(screen.getByTestId('mode')).toHaveTextContent('system:light')
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe('light'))
    expect(document.documentElement.style.colorScheme).toBe('light')
  })

  it('starts dark when the system prefers dark', () => {
    setSystemPrefersDark(true)
    render(
      <ThemeProvider>
        <Probe />
      </ThemeProvider>,
    )
    expect(screen.getByTestId('mode')).toHaveTextContent('system:dark')
  })

  it('reacts to a system colour change while in system mode', async () => {
    render(
      <ThemeProvider>
        <Probe />
      </ThemeProvider>,
    )

    await act(async () => setSystemPrefersDark(true))
    expect(screen.getByTestId('mode')).toHaveTextContent('system:dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
  })

  it('ignores a system change once a mode is chosen', async () => {
    const { result } = renderHook(() => useThemeMode(), {
      wrapper: ({ children }) => <ThemeProvider>{children}</ThemeProvider>,
    })

    act(() => result.current.setMode('light'))
    await act(async () => setSystemPrefersDark(true))

    expect(result.current.mode).toBe('light')
    expect(result.current.resolved).toBe('light')
  })

  it('restores the stored mode and persists a new one', async () => {
    window.localStorage.setItem('oa-theme', 'dark')
    const { result } = renderHook(() => useThemeMode(), {
      wrapper: ({ children }) => <ThemeProvider>{children}</ThemeProvider>,
    })

    expect(result.current.mode).toBe('dark')
    expect(result.current.resolved).toBe('dark')

    act(() => result.current.setMode('light'))
    expect(window.localStorage.getItem('oa-theme')).toBe('light')
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe('light'))
  })

  it('publishes design tokens as CSS variables', async () => {
    render(
      <ThemeProvider>
        <span>content</span>
      </ThemeProvider>,
    )

    await waitFor(() => expect(document.documentElement.style.getPropertyValue('--oa-bg')).toBeTruthy())
    expect(document.documentElement.style.getPropertyValue('--oa-primary')).toBeTruthy()
    expect(document.documentElement.style.getPropertyValue('--oa-radius')).toBe('8px')
  })

  it('refuses to be used outside the provider', () => {
    // React logs the thrown error; keep the test output readable.
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    expect(() => renderHook(() => useThemeMode())).toThrow(
      'useThemeMode must be used inside ThemeProvider',
    )
    error.mockRestore()
  })
})

describe('ThemeToggle', () => {
  it('shows the current mode in its accessible name', () => {
    render(
      <ThemeProvider>
        <ThemeToggle />
      </ThemeProvider>,
    )
    expect(screen.getByRole('button', { name: '主题：跟随系统' })).toBeInTheDocument()
  })

  it('offers the three modes and switches to the chosen one', async () => {
    const user = userEvent.setup()
    render(
      <ThemeProvider>
        <ThemeToggle compact={false} />
      </ThemeProvider>,
    )
    expect(screen.getByRole('button', { name: '主题：跟随系统' })).toHaveTextContent('跟随系统')

    await user.click(screen.getByRole('button', { name: '主题：跟随系统' }))
    await user.click(await screen.findByText('深色'))

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '主题：深色' })).toBeInTheDocument(),
    )
    expect(window.localStorage.getItem('oa-theme')).toBe('dark')
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe('dark'))

    await user.click(screen.getByRole('button', { name: '主题：深色' }))
    await user.click(await screen.findByText('浅色'))
    await waitFor(() =>
      expect(screen.getByRole('button', { name: '主题：浅色' })).toBeInTheDocument(),
    )
  })
})
