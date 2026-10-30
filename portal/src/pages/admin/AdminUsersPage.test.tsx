import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { fail, mockApi, ok } from '../../test/http'
import { makeAdminUser, makeUser } from '../../test/fixtures'
import { renderApp } from '../../test/router'

const admin = makeUser({ id: 'u_admin', username: 'root', role: 'admin' })

function usersPage(users = [makeAdminUser()], total = users.length) {
  return ok({ users, total, limit: 50, offset: 0 })
}

function routes(overrides: Record<string, unknown> = {}) {
  return {
    'GET /api/portal/me': ok({ user: admin, devices: [] }),
    'GET /api/admin/users': usersPage(),
    'POST /api/admin/users/u_1/status': ok({ user: makeUser({ status: 'disabled' }) }),
    ...overrides,
  }
}

// The table's sidebar makes a name-based role query take seconds; plain text
// lookup finds the row at once, and every later query is scoped to it.
async function rowOf(name: string): Promise<HTMLElement> {
  return (await screen.findByText(name)).closest('tr') as HTMLElement
}

async function openUsers() {
  const result = await renderApp('/admin/users')
  await rowOf('ada')
  return result
}

describe('AdminUsersPage', () => {
  it('lists users with their agents and app counts', async () => {
    mockApi(routes())
    await openUsers()

    const row = await rowOf('ada')
    expect(within(row).getByText('u_1')).toBeInTheDocument()
    expect(within(row).getByText('普通用户')).toBeInTheDocument()
    expect(within(row).getByText('启用')).toBeInTheDocument()
    expect(within(row).getByText('小灵')).toBeInTheDocument()
    expect(within(row).getByText('registered')).toBeInTheDocument()
    expect(within(row).getByText('3')).toBeInTheDocument()
    expect(within(row).getByRole('button', { name: /详\s*情/ }).closest('a')).toHaveAttribute(
      'href',
      '/admin/users/u_1',
    )
    expect(screen.getByText('1 个用户')).toBeInTheDocument()
  })

  it('shows a user without an agent', async () => {
    mockApi(
      routes({
        'GET /api/admin/users': usersPage([
          makeAdminUser({ id: 'u_2', username: 'bob', role: 'admin', agents: [], app_count: 0 }),
        ]),
      }),
    )
    await renderApp('/admin/users')

    const row = await rowOf('bob')
    expect(within(row).getByText('未创建')).toBeInTheDocument()
    expect(within(row).getByText('—')).toBeInTheDocument()
    expect(within(row).getByText('管理员')).toBeInTheDocument()
  })

  it('searches by name and resets paging', async () => {
    const user = userEvent.setup()
    const http = mockApi(routes())
    await openUsers()

    await user.type(screen.getByPlaceholderText('搜索用户名'), 'ada{Enter}')

    await waitFor(() => {
      const calls = http.callsFor('GET', '/api/admin/users')
      expect(calls.at(-1)?.url.searchParams.get('query')).toBe('ada')
      expect(calls.at(-1)?.url.searchParams.get('offset')).toBeNull()
    })
  })

  it('filters by account status', async () => {
    const user = userEvent.setup()
    const http = mockApi(routes())
    await openUsers()

    await user.click(screen.getByRole('combobox', { name: '按状态筛选' }))
    const dropdown = document.querySelector('.ant-select-dropdown') as HTMLElement
    await waitFor(() => expect(within(dropdown).getByText('停用')).toBeInTheDocument())
    await user.click(within(dropdown).getByText('停用'))

    await waitFor(() =>
      expect(http.callsFor('GET', '/api/admin/users').at(-1)?.url.searchParams.get('status')).toBe(
        'disabled',
      ),
    )
  })

  it('moves to the next page', async () => {
    const user = userEvent.setup()
    const http = mockApi(routes({ 'GET /api/admin/users': usersPage([makeAdminUser()], 60) }))
    await openUsers()

    expect(screen.getByText('60 个用户')).toBeInTheDocument()
    await user.click(screen.getByTitle('2'))

    await waitFor(() =>
      expect(http.callsFor('GET', '/api/admin/users').at(-1)?.url.searchParams.get('offset')).toBe(
        '50',
      ),
    )
  })

  it('disables an account after confirmation', async () => {
    const user = userEvent.setup()
    const http = mockApi(routes())
    await openUsers()

    const row = await rowOf('ada')
    await user.click(within(row).getByRole('button', { name: /停\s*用/ }))
    await user.click(await screen.findByText('确 认'))

    await waitFor(() =>
      expect(http.callsFor('POST', '/api/admin/users/u_1/status')).toHaveLength(1),
    )
    expect(http.callsFor('POST', '/api/admin/users/u_1/status')[0].body).toEqual({
      status: 'disabled',
    })
    expect(await screen.findByText('用户已停用')).toBeInTheDocument()
  })

  it('enables a disabled account', async () => {
    const user = userEvent.setup()
    const http = mockApi(
      routes({
        'GET /api/admin/users': usersPage([makeAdminUser({ status: 'disabled' })]),
      }),
    )
    await openUsers()

    const row = await rowOf('ada')
    expect(within(row).getByText('停用')).toBeInTheDocument()
    await user.click(within(row).getByRole('button', { name: /启\s*用/ }))
    await user.click(await screen.findByText('确 认'))

    await waitFor(() =>
      expect(http.callsFor('POST', '/api/admin/users/u_1/status')[0]?.body).toEqual({
        status: 'active',
      }),
    )
  })

  it('does not let an administrator change their own account here', async () => {
    mockApi(
      routes({
        'GET /api/portal/me': ok({ user: makeUser({ id: 'u_1', username: 'root', role: 'admin' }), devices: [] }),
      }),
    )
    await openUsers()

    const row = await rowOf('ada')
    expect(within(row).getByRole('button', { name: /停\s*用/ })).toBeDisabled()
  })

  it('reports a failed status change', async () => {
    const user = userEvent.setup()
    mockApi(
      routes({
        'POST /api/admin/users/u_1/status': fail(409, 'CONFLICT', '不能停用唯一的其他管理员'),
      }),
    )
    await openUsers()

    const row = await rowOf('ada')
    await user.click(within(row).getByRole('button', { name: /停\s*用/ }))
    await user.click(await screen.findByText('确 认'))

    expect(await screen.findByText('不能停用唯一的其他管理员')).toBeInTheDocument()
  })

  it('reports a failed list', async () => {
    mockApi(routes({ 'GET /api/admin/users': fail(500, 'FAILED', '用户表不可用') }))
    await renderApp('/admin/users')

    expect(await screen.findByText('无法加载用户列表')).toBeInTheDocument()
    expect(screen.getByText('用户表不可用')).toBeInTheDocument()
  })
})
