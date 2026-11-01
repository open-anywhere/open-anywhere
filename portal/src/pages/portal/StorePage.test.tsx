import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import type { StoreItem } from '../../api'
import { makeDevice, makeUser } from '../../test/fixtures'
import { mockApi, ok } from '../../test/http'
import { renderApp } from '../../test/router'

function makeStoreItem(overrides: Partial<StoreItem> = {}): StoreItem {
  return {
    store_id: 'app_1',
    title: '电影知识竞赛',
    summary: '五个人一起玩的电影抢答',
    app_name: 'movie-quiz',
    operation_count: 2,
    published_at: '2026-10-02T10:00:00Z',
    ...overrides,
  }
}

function routes(items: StoreItem[] = [makeStoreItem()], overrides: Record<string, unknown> = {}) {
  return {
    'GET /api/portal/me': ok({ user: makeUser(), devices: [makeDevice()] }),
    'GET /api/portal/store': ok({ items }),
    'POST /api/portal/store/app_1/clone': ok(
      { app_id: 'app_clone', name: '周末答题夜', state: 'idle' },
      201,
    ),
    'GET /api/portal/apps/app_clone': ok({ app: { app_id: 'app_clone' } }),
    ...overrides,
  }
}

describe('StorePage', () => {
  it('lists approved applications', async () => {
    mockApi(routes())
    await renderApp('/portal/store')

    expect(await screen.findByRole('heading', { name: '电影知识竞赛' })).toBeInTheDocument()
    expect(screen.getByText('五个人一起玩的电影抢答')).toBeInTheDocument()
    expect(screen.getByText('2 个操作')).toBeInTheDocument()
  })

  it('installs onto the chosen device with the name the user typed', async () => {
    const user = userEvent.setup()
    const http = mockApi(routes())
    await renderApp('/portal/store')

    await user.click(await screen.findByRole('button', { name: '添加到我的应用' }))
    const name = await screen.findByLabelText('应用名字')
    await user.clear(name)
    await user.type(name, '周末答题夜')
    await user.click(screen.getByRole('button', { name: '添 加' }))

    await waitFor(() => {
      const call = http.callsFor('POST', '/api/portal/store/app_1/clone')[0]
      expect(call?.body).toEqual({ name: '周末答题夜', device_id: 'dev_1' })
    })
  })

  it('searches the store by keyword', async () => {
    const user = userEvent.setup()
    const http = mockApi(routes())
    await renderApp('/portal/store')

    await screen.findByRole('heading', { name: '电影知识竞赛' })
    await user.type(screen.getByPlaceholderText('搜索标题或简介'), '答题{Enter}')

    await waitFor(() =>
      expect(http.callsFor('GET', '/api/portal/store').at(-1)?.url.searchParams.get('query')).toBe(
        '答题',
      ),
    )
  })

  it('explains an empty store', async () => {
    mockApi(routes([]))
    await renderApp('/portal/store')

    expect(await screen.findByText('商城里还没有应用')).toBeInTheDocument()
  })

  it('asks for a device when the account has none registered', async () => {
    const user = userEvent.setup()
    mockApi(
      routes([makeStoreItem()], {
        'GET /api/portal/me': ok({
          user: makeUser(),
          devices: [makeDevice({ state: 'provisioning', agent_id: '' })],
        }),
      }),
    )
    await renderApp('/portal/store')

    await user.click(await screen.findByRole('button', { name: '添加到我的应用' }))
    expect(await screen.findByText('还没有可用的设备')).toBeInTheDocument()
  })
})
