import { QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { ThemeProvider } from '../components/ThemeProvider'
import { mockApi, ok, fail } from '../test/http'
import { makeDevice } from '../test/fixtures'
import { makeQueryClient } from '../test/router'
import { useDeviceConnection } from './useDeviceConnection'

function setup(client = makeQueryClient()) {
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <ThemeProvider>
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      </ThemeProvider>
    )
  }
  const hook = renderHook(() => useDeviceConnection(), { wrapper: Wrapper })
  return { ...hook, queryClient: client }
}

describe('useDeviceConnection', () => {
  it('disconnects and refreshes the device list', async () => {
    const http = mockApi({
      'POST /api/portal/devices/dev_1/disconnect': ok({ device: makeDevice() }),
    })
    const { result, queryClient } = setup()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')

    act(() => result.current.disconnect.mutate('dev_1'))
    await waitFor(() => expect(http.calls).toHaveLength(1))
    await screen.findByText('已断开连接')
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['me'] })
  })

  it('reconnects and refreshes the device list', async () => {
    const http = mockApi({
      'POST /api/portal/devices/dev_1/reconnect': ok({ device: makeDevice() }),
    })
    const { result, queryClient } = setup()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')

    act(() => result.current.reconnect.mutate('dev_1'))
    await waitFor(() => expect(http.calls).toHaveLength(1))
    await screen.findByText('已重新连接')
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['me'] })
  })

  it('shows the gateway message when disconnecting fails', async () => {
    mockApi({
      'POST /api/portal/devices/dev_1/disconnect': fail(409, 'BUSY', '智能体正在进行对话'),
    })
    const { result } = setup()

    act(() => result.current.disconnect.mutate('dev_1'))
    await screen.findByText('智能体正在进行对话')
  })

  it('falls back to a generic message for a non-API failure', async () => {
    mockApi({
      'POST /api/portal/devices/dev_1/reconnect': () => {
        throw new Error('socket closed')
      },
    })
    const { result } = setup()

    act(() => result.current.reconnect.mutate('dev_1'))
    await screen.findByText('暂时无法重新连接')
  })
})
