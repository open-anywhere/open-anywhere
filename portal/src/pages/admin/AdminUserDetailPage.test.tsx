import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { fail, mockApi, ok } from '../../test/http'
import { makeAdminAgent, makeAdminApp, makeAdminUser, makeUser } from '../../test/fixtures'
import { renderApp } from '../../test/router'

const admin = makeUser({ id: 'u_admin', username: 'root', role: 'admin' })

function routes(overrides: Record<string, unknown> = {}) {
  return {
    'GET /api/portal/me': ok({ user: admin, devices: [] }),
    'GET /api/admin/users/u_1': ok({
      user: makeAdminUser(),
      apps: [makeAdminApp()],
    }),
    'POST /api/admin/users/u_1/status': ok({ user: makeUser({ status: 'disabled' }) }),
    'POST /api/admin/agents/agent_device_1/retry': ok({ agent: makeAdminAgent() }),
    ...overrides,
  }
}

async function openDetail() {
  const result = await renderApp('/admin/users/u_1')
  await screen.findAllByText('ada')
  return result
}

describe('AdminUserDetailPage', () => {
  it('shows the account, its agents and its apps', async () => {
    mockApi(routes())
    await openDetail()

    expect(screen.getByText('u_1')).toBeInTheDocument()
    expect(screen.getByText('普通用户')).toBeInTheDocument()
    expect(screen.getByText('启用')).toBeInTheDocument()
    expect(screen.getByText('小灵')).toBeInTheDocument()
    expect(screen.getByText('agent_1')).toBeInTheDocument()
    expect(screen.getByText('AA:BB:CC:DD:EE:01')).toBeInTheDocument()
    expect(screen.getAllByText('已准备好').length).toBeGreaterThan(0)
    expect(screen.getByText('电影知识竞赛')).toBeInTheDocument()
    expect(screen.getByText('app_1')).toBeInTheDocument()
    expect(screen.getByText('屏幕上')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /返回用户列表/ }).closest('a')).toHaveAttribute(
      'href',
      '/admin/users',
    )
  })

  it('says when the user has no agent or app yet', async () => {
    mockApi(
      routes({
        'GET /api/admin/users/u_1': ok({
          user: makeAdminUser({ agents: [] }),
          apps: [],
        }),
      }),
    )
    await openDetail()

    expect(screen.getByText('尚未创建智能体')).toBeInTheDocument()
    expect(screen.getByText('还没有应用')).toBeInTheDocument()
  })

  it('retries a failed provisioning', async () => {
    const user = userEvent.setup()
    const http = mockApi(
      routes({
        'GET /api/admin/users/u_1': ok({
          user: makeAdminUser({
            agents: [
              makeAdminAgent({ state: 'error', step: 'bind', error: '灵矽返回 500' }),
            ],
          }),
          apps: [],
        }),
      }),
    )
    await openDetail()

    expect(screen.getByText('灵矽返回 500')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '重试开通' }))

    await waitFor(() =>
      expect(http.callsFor('POST', '/api/admin/agents/agent_device_1/retry')).toHaveLength(1),
    )
    expect(await screen.findByText('已重新开始开通流程')).toBeInTheDocument()
  })

  it('reports a failed retry', async () => {
    const user = userEvent.setup()
    mockApi(
      routes({
        'POST /api/admin/agents/agent_device_1/retry': fail(502, 'UPSTREAM', '灵矽暂时不可用'),
      }),
    )
    await openDetail()

    await user.click(screen.getByRole('button', { name: '重试开通' }))

    expect(await screen.findByText('灵矽暂时不可用')).toBeInTheDocument()
  })

  it('disables the account after confirmation', async () => {
    const user = userEvent.setup()
    const http = mockApi(routes())
    await openDetail()

    await user.click(screen.getByRole('button', { name: '停用账号' }))
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
        'GET /api/admin/users/u_1': ok({
          user: makeAdminUser({ status: 'disabled' }),
          apps: [],
        }),
      }),
    )
    await openDetail()

    expect(screen.getByText('停用')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '启用账号' }))
    await user.click(await screen.findByText('确 认'))

    await waitFor(() =>
      expect(http.callsFor('POST', '/api/admin/users/u_1/status')[0]?.body).toEqual({
        status: 'active',
      }),
    )
  })

  it('reports a failed detail load', async () => {
    mockApi(routes({ 'GET /api/admin/users/u_1': fail(404, 'NOT_FOUND', '用户不存在') }))
    await renderApp('/admin/users/u_1')

    expect(await screen.findByText('无法加载用户详情')).toBeInTheDocument()
    expect(screen.getByText('用户不存在')).toBeInTheDocument()
  })
})