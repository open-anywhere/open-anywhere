import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { fail, mockApi, ok } from '../../test/http'
import { makeDevice, makeScreen, makeUser } from '../../test/fixtures'
import { renderApp } from '../../test/router'

function routes(overrides: Record<string, unknown> = {}) {
  return {
    'GET /api/portal/screens': ok({
      screens: [
        makeScreen({ screen_id: 'screen_1', name: '客厅电视', status: 'free' }),
        makeScreen({
          screen_id: 'screen_2',
          name: '书房投影',
          status: 'showing_app',
          current_app: { app_id: 'app_1', name: '电影知识竞赛' },
        }),
      ],
    }),
    'GET /api/portal/me': ok({ user: makeUser(), devices: [makeDevice()] }),
    'GET /api/portal/apps': ok({ apps: [] }),
    'POST /api/portal/screens/screen_2/stop': ok({
      screen_id: 'screen_2',
      app: { app_id: 'app_1', state: 'idle' },
    }),
    ...overrides,
  }
}

describe('ScreensPage', () => {
  it('lists screens and the app each one plays', async () => {
    mockApi(routes())
    await renderApp('/portal/screens')

    expect(await screen.findByText('客厅电视')).toBeInTheDocument()
    expect(screen.getByText('书房投影')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '电影知识竞赛' })).toHaveAttribute(
      'href',
      '/portal/apps/app_1',
    )
    expect(screen.getAllByText('屏幕会自动被发现。想让屏幕播放什么，开口说出来就好。')).toHaveLength(1)
    // Only the screen that is playing can be stopped.
    expect(screen.getAllByRole('button', { name: /停止播放/ })).toHaveLength(1)
  })

  it('stops playback after confirmation', async () => {
    const user = userEvent.setup()
    const http = mockApi(routes())
    await renderApp('/portal/screens')

    await user.click(await screen.findByRole('button', { name: /停止播放/ }))
    const confirms = await screen.findAllByRole('button', { name: /停止播放/ })
    await user.click(confirms.at(-1) as HTMLElement)

    await waitFor(() =>
      expect(http.callsFor('POST', '/api/portal/screens/screen_2/stop')).toHaveLength(1),
    )
  })

  it('invites the owner to look for screens when none are found', async () => {
    const user = userEvent.setup()
    mockApi(routes({ 'GET /api/portal/screens': ok({ screens: [] }) }))
    await renderApp('/portal/screens')

    expect(await screen.findByText('还没有发现屏幕')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '检查屏幕连接' })).toHaveAttribute(
      'href',
      '/portal/services',
    )
    await user.click(screen.getByRole('button', { name: /再找一次/ }))
    await waitFor(() => expect(screen.queryByText('还没有发现屏幕')).not.toBeNull())
  })

  it('recovers after a failed discovery', async () => {
    const user = userEvent.setup()
    let calls = 0
    mockApi(
      routes({
        'GET /api/portal/screens': () =>
          calls++ === 0
            ? fail(503, 'UNAVAILABLE', '投屏服务没有响应')
            : ok({ screens: [makeScreen({ name: '客厅电视' })] }),
      }),
    )
    await renderApp('/portal/screens')

    expect(await screen.findByText('暂时找不到屏幕')).toBeInTheDocument()
    expect(screen.getByText('投屏服务没有响应')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /重\s*试/ }))
    expect(await screen.findByText('客厅电视')).toBeInTheDocument()
    expect(screen.queryByText('暂时找不到屏幕')).toBeNull()
  })
})
