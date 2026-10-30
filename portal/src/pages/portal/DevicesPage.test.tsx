import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { fail, mockApi, ok } from '../../test/http'
import { makeDevice, makeUser } from '../../test/fixtures'
import { renderApp } from '../../test/router'

function routes(devices = [makeDevice()], overrides: Record<string, unknown> = {}) {
  return {
    'GET /api/portal/me': ok({ user: makeUser(), devices }),
    'GET /api/portal/apps': ok({ apps: [] }),
    ...overrides,
  }
}

function row(name: string): HTMLElement {
  return screen.getByText(name).closest('tr') as HTMLElement
}

describe('DevicesPage', () => {
  it('lists a device with its agent and connection state', async () => {
    mockApi(routes())
    await renderApp('/portal/devices')

    expect(await screen.findByText('客厅')).toBeInTheDocument()
    expect(screen.getByText('虚拟')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '小灵' })).toHaveAttribute('href', '/portal/agents')

    const cells = within(row('客厅'))
    expect(cells.getByRole('button', { name: /对\s*话/ })).toBeEnabled()
    expect(cells.getByRole('button', { name: /重新连接/ })).toBeInTheDocument()
    expect(cells.getByRole('button', { name: /断\s*开/ })).toBeInTheDocument()
    // A single device cannot be removed: it is the account's only agent.
    expect(cells.queryByRole('button', { name: /移\s*除/ })).toBeNull()
  })

  it('disables the call button while the agent is offline', async () => {
    mockApi(
      routes([
        makeDevice({ voice: { state: 'offline', connected: false } }),
        makeDevice({ id: 'dev_2', name: '书房', mac: 'AA:BB:CC:DD:EE:02' }),
      ]),
    )
    await renderApp('/portal/devices')

    const cells = within(await screen.findByText('客厅').then((node) => node.closest('tr') as HTMLElement))
    expect(cells.getByRole('button', { name: /对\s*话/ })).toBeDisabled()
    expect(cells.queryByRole('button', { name: /断\s*开/ })).toBeNull()
  })

  it('binds hardware through the add dialog', async () => {
    const user = userEvent.setup()
    const http = mockApi(
      routes([makeDevice()], {
        'POST /api/portal/devices': ok({ device: makeDevice({ id: 'dev_2', kind: 'physical' }) }),
      }),
    )
    await renderApp('/portal/devices')

    await user.click(await screen.findByRole('button', { name: /添加设备/ }))
    const confirm = await screen.findByRole('button', { name: '绑定设备' })
    await user.type(screen.getByPlaceholderText('例如 123456'), 'ABC123')
    await user.click(confirm)

    await waitFor(() => expect(http.callsFor('POST', '/api/portal/devices')).toHaveLength(1))
    expect(http.callsFor('POST', '/api/portal/devices')[0].body).toEqual({
      kind: 'physical',
      name: undefined,
      activation_code: 'ABC123',
      agent_device_id: 'dev_1',
    })
  })

  it('asks before force-removing a device the platform could not unbind', async () => {
    const user = userEvent.setup()
    let attempts = 0
    const http = mockApi(
      routes(
        [makeDevice(), makeDevice({ id: 'dev_2', name: '书房', mac: 'AA:BB:CC:DD:EE:02' })],
        {
          'DELETE /api/portal/devices/dev_2': () =>
            attempts++ === 0
              ? fail(409, 'UNBIND_FAILED', '灵矽拒绝了这次解绑')
              : ok(null, 204),
        },
      ),
    )
    await renderApp('/portal/devices')

    await user.click(within(row('书房')).getByRole('button', { name: /移\s*除/ }))
    const confirms = await screen.findAllByText('移 除')
    await user.click(confirms.at(-1) as HTMLElement)

    expect((await screen.findAllByText('向灵矽解绑失败')).length).toBeGreaterThan(0)
    expect(screen.getByText(/仍要从这里移除吗/)).toBeInTheDocument()
    await user.click(await screen.findByText('强制移除'))

    await waitFor(() => expect(http.callsFor('DELETE', '/api/portal/devices/dev_2')).toHaveLength(2))
    const [first, second] = http.callsFor('DELETE', '/api/portal/devices/dev_2')
    expect(first.url.searchParams.get('force')).toBeNull()
    expect(second.url.searchParams.get('force')).toBe('true')
  })
})
