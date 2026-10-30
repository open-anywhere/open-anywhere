import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { fail, mockApi, ok } from '../../test/http'
import { makeDevice, makeServices, makeUser } from '../../test/fixtures'
import { renderApp } from '../../test/router'

function routes(services = makeServices(), overrides: Record<string, unknown> = {}) {
  return {
    'GET /api/portal/me': ok({ user: makeUser(), devices: [makeDevice()] }),
    'GET /api/portal/services': ok(services),
    'PUT /api/portal/services': ok(makeServices({ mode: 'custom' })),
    'POST /api/portal/services/test': ok({
      render: { ok: true },
      casting: { ok: true },
      screens: 2,
    }),
    ...overrides,
  }
}

async function switchToCustom() {
  const user = userEvent.setup()
  await screen.findByText('屏幕连接已就绪')
  await user.click(screen.getByText('自己接入'))
  await screen.findByLabelText('运行服务地址')
  return user
}

describe('ServicesPage', () => {
  it('reports the platform services as ready', async () => {
    mockApi(routes())
    await renderApp('/portal/services')

    expect(await screen.findByText('屏幕连接已就绪')).toBeInTheDocument()
    expect(screen.getByText('已配置')).toBeInTheDocument()
    expect(screen.getByText('https://render.example.com')).toBeInTheDocument()
    expect(screen.getByText('https://casting.example.com')).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: /平台提供/ })).toBeChecked()
  })

  it('warns while the services are not ready', async () => {
    mockApi(routes(makeServices({ ready: false })))
    await renderApp('/portal/services')

    expect(await screen.findByText('屏幕连接还不可用')).toBeInTheDocument()
    expect(
      screen.getByText('应用暂时还不能上屏。可以检查下面的设置，或稍后再试。'),
    ).toBeInTheDocument()
  })

  it('locks the managed mode when the platform has no services', async () => {
    mockApi(
      routes(
        makeServices({
          managed: { available: false, render_url: '', casting_url: '' },
        }),
      ),
    )
    await renderApp('/portal/services')

    expect(await screen.findByText('平台服务还没准备好')).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: /平台提供/ })).toBeDisabled()
    expect(screen.getByRole('button', { name: /保存设置/ })).toBeDisabled()
  })

  it('saves custom addresses', async () => {
    const http = mockApi(routes())
    await renderApp('/portal/services')
    const user = await switchToCustom()

    await user.type(screen.getByLabelText('运行服务地址'), 'https://render.mine.test')
    await user.type(screen.getByLabelText('投屏服务地址'), 'https://casting.mine.test')
    await user.click(screen.getByRole('button', { name: /保存设置/ }))

    await waitFor(() => expect(http.callsFor('PUT', '/api/portal/services')).toHaveLength(1))
    expect(http.callsFor('PUT', '/api/portal/services')[0].body).toEqual({
      mode: 'custom',
      render_url: 'https://render.mine.test',
      casting_url: 'https://casting.mine.test',
    })
  })

  it('requires both addresses in custom mode', async () => {
    const http = mockApi(routes())
    await renderApp('/portal/services')
    const user = await switchToCustom()

    await user.click(screen.getByRole('button', { name: /保存设置/ }))

    expect(await screen.findByText('请输入运行服务地址')).toBeInTheDocument()
    expect(screen.getByText('请输入投屏服务地址')).toBeInTheDocument()
    expect(http.callsFor('PUT', '/api/portal/services')).toHaveLength(0)
  })

  it('tests the current form values without saving', async () => {
    const user = userEvent.setup()
    const http = mockApi(routes())
    await renderApp('/portal/services')

    await screen.findByText('屏幕连接已就绪')
    await user.click(screen.getByRole('button', { name: /测试连接/ }))

    await waitFor(() =>
      expect(http.callsFor('POST', '/api/portal/services/test')).toHaveLength(1),
    )
    expect(http.callsFor('POST', '/api/portal/services/test')[0].body).toEqual({ mode: 'managed' })
    expect(http.callsFor('PUT', '/api/portal/services')).toHaveLength(0)
    expect(await screen.findByText('连接测试')).toBeInTheDocument()
    expect(
      screen.getByText('运行服务可用 · 投屏服务可用 · 发现 2 块屏幕'),
    ).toBeInTheDocument()
  })

  it('explains a failed read', async () => {
    mockApi(routes(makeServices(), { 'GET /api/portal/services': fail(502, 'UNAVAILABLE', '网关没有响应') }))
    await renderApp('/portal/services')

    expect(await screen.findByText('暂时无法读取屏幕连接')).toBeInTheDocument()
    expect(screen.getByText('网关没有响应')).toBeInTheDocument()
  })
})
