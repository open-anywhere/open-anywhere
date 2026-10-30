import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { fail, mockApi, ok } from '../../test/http'
import { makeAdminAgent, makeUser } from '../../test/fixtures'
import { renderApp } from '../../test/router'

function agentsPage(agents = [makeAdminAgent()]) {
  return ok({ agents })
}

function routes(overrides: Record<string, unknown> = {}) {
  return {
    'GET /api/portal/me': ok({ user: makeUser({ role: 'admin' }), devices: [] }),
    'GET /api/admin/agents': agentsPage(),
    'POST /api/admin/agents/agent_device_1/retry': ok({ agent: makeAdminAgent() }),
    ...overrides,
  }
}

async function openAgents() {
  const result = await renderApp('/admin/agents')
  await screen.findByText('小灵')
  return result
}

describe('AdminAgentsPage', () => {
  it('lists agents with their owner and provisioning step', async () => {
    mockApi(routes())
    await openAgents()

    const row = screen.getByText('小灵').closest('tr') as HTMLElement
    expect(within(row).getByText('agent_1')).toBeInTheDocument()
    expect(within(row).getByText('ada')).toBeInTheDocument()
    expect(within(row).getByText('已准备好')).toBeInTheDocument()
    expect(within(row).getByText('完成')).toBeInTheDocument()
    expect(within(row).getByRole('link', { name: 'ada' })).toHaveAttribute(
      'href',
      '/admin/users/u_1',
    )
    expect(screen.getByText('1 个智能体')).toBeInTheDocument()
  })

  it('shows an agent that never finished provisioning', async () => {
    mockApi(
      routes({
        'GET /api/admin/agents': agentsPage([
          makeAdminAgent({ agent_name: '', agent_id: '', state: 'provisioning', step: 'attach_mcp' }),
        ]),
      }),
    )
    await renderApp('/admin/agents')

    expect(await screen.findByText('未命名智能体')).toBeInTheDocument()
    expect(screen.getByText('尚未分配')).toBeInTheDocument()
    expect(screen.getByText('挂载 MCP')).toBeInTheDocument()
  })

  it('shows the failure reason instead of the step', async () => {
    mockApi(
      routes({
        'GET /api/admin/agents': agentsPage([
          makeAdminAgent({ state: 'error', step: 'ota', error: 'OTA 超时' }),
        ]),
      }),
    )
    await openAgents()

    expect(screen.getByText('OTA 超时')).toBeInTheDocument()
    expect(screen.queryByText('注册设备')).toBeNull()
  })

  it('searches agents by name or id', async () => {
    const user = userEvent.setup()
    const http = mockApi(routes())
    await openAgents()

    await user.type(screen.getByPlaceholderText('搜索用户名、MAC 或智能体 ID'), 'ada{Enter}')

    await waitFor(() =>
      expect(http.callsFor('GET', '/api/admin/agents').at(-1)?.url.searchParams.get('query')).toBe(
        'ada',
      ),
    )
  })

  it('filters by agent state', async () => {
    const user = userEvent.setup()
    const http = mockApi(routes())
    await openAgents()

    await user.click(screen.getByRole('combobox', { name: '按状态筛选' }))
    const dropdown = document.querySelector('.ant-select-dropdown') as HTMLElement
    await waitFor(() => expect(within(dropdown).getByText('失败')).toBeInTheDocument())
    await user.click(within(dropdown).getByText('失败'))

    await waitFor(() =>
      expect(http.callsFor('GET', '/api/admin/agents').at(-1)?.url.searchParams.get('state')).toBe(
        'error',
      ),
    )
  })

  it('retries provisioning', async () => {
    const user = userEvent.setup()
    const http = mockApi(routes())
    await openAgents()

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
    await openAgents()

    await user.click(screen.getByRole('button', { name: '重试开通' }))

    expect(await screen.findByText('灵矽暂时不可用')).toBeInTheDocument()
  })

  it('reports a failed list', async () => {
    mockApi(routes({ 'GET /api/admin/agents': fail(500, 'FAILED', '智能体表不可用') }))
    await renderApp('/admin/agents')

    expect(await screen.findByText('无法加载智能体列表')).toBeInTheDocument()
    expect(screen.getByText('智能体表不可用')).toBeInTheDocument()
  })
})
