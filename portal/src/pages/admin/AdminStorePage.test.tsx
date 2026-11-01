import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import type { AdminStoreItem } from '../../api'
import { makeUser } from '../../test/fixtures'
import { mockApi, ok } from '../../test/http'
import { renderApp } from '../../test/router'

function makeStoreEntry(overrides: Partial<AdminStoreItem> = {}): AdminStoreItem {
  return {
    store_id: 'app_1',
    title: '电影知识竞赛',
    summary: '五个人一起玩的电影抢答',
    app_name: 'movie-quiz',
    operation_count: 2,
    published_at: '2026-10-02T10:00:00Z',
    updated_at: '2026-10-02T10:00:00Z',
    status: 'pending',
    owner_user_id: 'u_1',
    username: 'ada',
    ...overrides,
  }
}

function routes(items: AdminStoreItem[] = [makeStoreEntry()], overrides: Record<string, unknown> = {}) {
  return {
    'GET /api/portal/me': ok({ user: makeUser({ role: 'admin' }), devices: [] }),
    'GET /api/admin/store': ok({ items }),
    'POST /api/admin/store/app_1/review': ok({ publication: { status: 'approved' } }),
    ...overrides,
  }
}

describe('AdminStorePage', () => {
  it('lists the review queue with author and status', async () => {
    mockApi(routes())
    await renderApp('/admin/store')

    const row = (await screen.findByText('电影知识竞赛')).closest('tr') as HTMLElement
    expect(within(row).getByText('ada')).toBeInTheDocument()
    expect(within(row).getByText('待审核')).toBeInTheDocument()
  })

  it('approves an entry', async () => {
    const user = userEvent.setup()
    const http = mockApi(routes())
    await renderApp('/admin/store')

    await user.click(await screen.findByRole('button', { name: /通\s*过/ }))

    await waitFor(() => {
      expect(http.callsFor('POST', '/api/admin/store/app_1/review')[0]?.body).toEqual({
        decision: 'approve',
        note: '',
      })
    })
  })

  it('rejects with a note for the author', async () => {
    const user = userEvent.setup()
    const http = mockApi(routes())
    await renderApp('/admin/store')

    await user.click(await screen.findByRole('button', { name: /拒\s*绝/ }))
    await user.type(await screen.findByPlaceholderText('写给作者的原因（可选）'), '标题太笼统')
    await user.click(screen.getByRole('button', { name: '确认拒绝' }))

    await waitFor(() => {
      expect(http.callsFor('POST', '/api/admin/store/app_1/review')[0]?.body).toEqual({
        decision: 'reject',
        note: '标题太笼统',
      })
    })
  })

  it('disables the action that matches the current status', async () => {
    mockApi(routes([makeStoreEntry({ status: 'approved' })]))
    await renderApp('/admin/store')

    expect(await screen.findByRole('button', { name: /通\s*过/ })).toBeDisabled()
    expect(screen.getByRole('button', { name: /拒\s*绝/ })).toBeEnabled()
  })
})
