import { describe, expect, it } from 'vitest'
import { ApiError, adminApi, api, voice } from './index'
import { fail, mockApi, ok } from '../test/http'
import { makeDevice, makeUser } from '../test/fixtures'

describe('ApiError', () => {
  it('carries the status, code and auth flag', () => {
    const error = new ApiError(401, 'UNAUTHORIZED', '请先登录', true)
    expect(error).toBeInstanceOf(Error)
    expect(error.name).toBe('ApiError')
    expect(error.status).toBe(401)
    expect(error.code).toBe('UNAUTHORIZED')
    expect(error.message).toBe('请先登录')
    expect(error.authRequest).toBe(true)
  })
})

describe('request', () => {
  it('sends GET requests with the session cookie and no content type', async () => {
    const me = { user: makeUser(), devices: [] }
    const http = mockApi({ 'GET /api/portal/me': ok(me) })

    await expect(api.me()).resolves.toEqual(me)

    const [url, init] = http.fetchMock.mock.calls[0]
    expect(url).toBe('/api/portal/me')
    expect(init?.credentials).toBe('same-origin')
    expect((init?.headers as Headers).get('Content-Type')).toBeNull()
    expect(http.calls[0].method).toBe('GET')
  })

  it('adds a JSON content type to every non-read request', async () => {
    const http = mockApi({
      'POST /api/portal/login': ok({ user: makeUser() }),
      'DELETE /api/portal/apps/app_1': { status: 204 },
    })

    await api.login('ada', 'password123')
    await api.deleteApp('app_1')

    const loginInit = http.fetchMock.mock.calls[0][1] as RequestInit
    expect((loginInit.headers as Headers).get('Content-Type')).toBe('application/json')
    expect(loginInit.body).toBe(JSON.stringify({ username: 'ada', password: 'password123' }))
    expect(http.calls[0].method).toBe('POST')

    const deleteInit = http.fetchMock.mock.calls[1][1] as RequestInit
    expect(http.calls[1].method).toBe('DELETE')
    expect(deleteInit.body).toBe('{}')
  })

  it('returns undefined for 204 without reading a body', async () => {
    mockApi({ 'DELETE /api/portal/apps/app_1': { status: 204 } })
    await expect(api.deleteApp('app_1')).resolves.toBeUndefined()
  })

  it('returns undefined when a successful response has no JSON body', async () => {
    mockApi({ 'GET /api/portal/me': { status: 200, text: 'not json' } })
    await expect(api.me()).resolves.toBeUndefined()
  })

  it('maps an error envelope onto ApiError', async () => {
    mockApi({ 'GET /api/portal/apps': fail(409, 'BUSY', '应用中还有正在播放的会话') })
    const error = await api.listApps().catch((value) => value)
    expect(error).toBeInstanceOf(ApiError)
    expect(error.status).toBe(409)
    expect(error.code).toBe('BUSY')
    expect(error.message).toBe('应用中还有正在播放的会话')
    expect(error.authRequest).toBe(false)
  })

  it('uses the raw body as the message when there is no envelope', async () => {
    mockApi({ 'GET /api/portal/apps': { status: 502, text: 'bad gateway' } })
    const error = await api.listApps().catch((value) => value)
    expect(error.message).toBe('bad gateway')
    expect(error.code).toBe('REQUEST_FAILED')
  })

  it('falls back to a generic message and code for an empty error', async () => {
    mockApi({ 'GET /api/portal/apps': { status: 500, text: '' } })
    const error = await api.listApps().catch((value) => value)
    expect(error.message).toBe('请求失败（HTTP 500）')
    expect(error.code).toBe('REQUEST_FAILED')
  })

  it('marks a 401 without a code as UNAUTHORIZED', async () => {
    mockApi({ 'GET /api/portal/apps': { status: 401, text: '' } })
    const error = await api.listApps().catch((value) => value)
    expect(error.code).toBe('UNAUTHORIZED')
  })

  it('marks login and register failures as auth requests', async () => {
    mockApi({
      'POST /api/portal/login': fail(401, 'INVALID', '密码不对'),
      'POST /api/portal/register': fail(409, 'TAKEN', '用户名已被占用'),
    })
    expect((await api.login('ada', 'password123').catch((value) => value)).authRequest).toBe(true)
    expect(
      (await api.register('ada', 'password123').catch((value) => value)).authRequest,
    ).toBe(true)
  })
})

describe('portal endpoints', () => {
  it('builds every device and app URL with encoded ids', async () => {
    const http = mockApi({
      'POST /api/portal/register': ok({ user: makeUser(), device: makeDevice() }),
      'POST /api/portal/logout': { status: 204 },
      'POST /api/portal/devices': ok({ device: makeDevice() }),
      'GET /api/portal/agent-options': ok({ voices: [] }),
      'GET /api/portal/devices/a%2Fb/agent': ok({ agent: {} }),
      'PUT /api/portal/devices/a%2Fb/agent': ok({ agent: {} }),
      'GET /api/portal/devices/a%2Fb': ok({ device: makeDevice() }),
      'DELETE /api/portal/devices/a%2Fb': { status: 204 },
      'POST /api/portal/devices/a%2Fb/retry': ok({ device: makeDevice() }),
      'POST /api/portal/devices/a%2Fb/disconnect': ok({ device: makeDevice() }),
      'POST /api/portal/devices/a%2Fb/reconnect': ok({ device: makeDevice() }),
      'GET /api/portal/apps': ok({ apps: [] }),
      'GET /api/portal/apps/a%2Fb': ok({ app: {} }),
      'DELETE /api/portal/apps/a%2Fb': { status: 204 },
      'GET /api/portal/services': ok({ mode: 'managed' }),
      'PUT /api/portal/services': ok({ mode: 'custom' }),
      'POST /api/portal/services/test': ok({ render: { ok: true } }),
      'GET /api/portal/devices/a%2Fb/mcps': ok({ required: null, available: [] }),
      'PUT /api/portal/devices/a%2Fb/mcps': ok({ ids: [] }),
    })

    await api.register('ada', 'password123')
    await api.logout()
    await api.createDevice({ kind: 'virtual', name: '客厅' })
    await api.agentOptions()
    await api.getAgentSettings('a/b')
    await api.updateAgentSettings('a/b', { assistant_name: '小灵' })
    await api.getDevice('a/b')
    await api.deleteDevice('a/b')
    await api.retryDevice('a/b')
    await api.disconnectDevice('a/b')
    await api.reconnectDevice('a/b')
    await api.listApps()
    await api.getApp('a/b')
    await api.deleteApp('a/b')
    await api.services()
    await api.updateServices({ mode: 'custom' })
    await api.testServices({ mode: 'managed' })
    await api.getDeviceMcps('a/b')
    await api.setDeviceMcps('a/b', ['mcp_1'])

    expect(http.calls.map((call) => `${call.method} ${call.path}`)).toEqual([
      'POST /api/portal/register',
      'POST /api/portal/logout',
      'POST /api/portal/devices',
      'GET /api/portal/agent-options',
      'GET /api/portal/devices/a%2Fb/agent',
      'PUT /api/portal/devices/a%2Fb/agent',
      'GET /api/portal/devices/a%2Fb',
      'DELETE /api/portal/devices/a%2Fb',
      'POST /api/portal/devices/a%2Fb/retry',
      'POST /api/portal/devices/a%2Fb/disconnect',
      'POST /api/portal/devices/a%2Fb/reconnect',
      'GET /api/portal/apps',
      'GET /api/portal/apps/a%2Fb',
      'DELETE /api/portal/apps/a%2Fb',
      'GET /api/portal/services',
      'PUT /api/portal/services',
      'POST /api/portal/services/test',
      'GET /api/portal/devices/a%2Fb/mcps',
      'PUT /api/portal/devices/a%2Fb/mcps',
    ])
    expect(http.unexpected).toEqual([])
  })

  it('adds the force flag when deleting a device forcibly', async () => {
    const http = mockApi({ 'DELETE /api/portal/devices/dev_1': { status: 204 } })
    await api.deleteDevice('dev_1', true)
    expect(http.calls[0].url.search).toBe('?force=true')
  })

  it('asks for the requested number of chat lines', async () => {
    const http = mockApi({
      'GET /api/portal/devices/dev_1/chat-history': ok({ records: [] }),
    })
    await api.getChatHistory('dev_1', 5)
    expect(http.calls[0].url.search).toBe('?limit=5')
  })
})

describe('openapi', () => {
  it('returns the document text', async () => {
    const http = mockApi({
      'GET /api/portal/apps/app_1/openapi.yaml': { text: 'openapi: 3.0.0' },
    })
    await expect(api.openapi('app_1')).resolves.toBe('openapi: 3.0.0')
    expect(http.calls[0].path).toBe('/api/portal/apps/app_1/openapi.yaml')
  })

  it('uses the error message from the gateway', async () => {
    mockApi({
      'GET /api/portal/apps/app_1/openapi.yaml': {
        status: 404,
        json: { error: { message: '文档还没有生成' } },
      },
    })
    const error = await api.openapi('app_1').catch((value) => value)
    expect(error).toBeInstanceOf(ApiError)
    expect(error.status).toBe(404)
    expect(error.code).toBe('OPENAPI_UNAVAILABLE')
    expect(error.message).toBe('文档还没有生成')
  })

  it('falls back to a Chinese message for a non-JSON error', async () => {
    mockApi({
      'GET /api/portal/apps/app_1/openapi.yaml': { status: 500, text: '<html>' },
    })
    const error = await api.openapi('app_1').catch((value) => value)
    expect(error.message).toBe('暂时无法读取接口文档（HTTP 500）')
  })

  it('builds the download URL', () => {
    expect(api.openapiDownloadURL('a/b')).toBe('/api/portal/apps/a%2Fb/openapi.yaml?download=1')
  })
})

describe('admin endpoints', () => {
  it('sends user filters as a query string and skips empty values', async () => {
    const http = mockApi({
      'GET /api/admin/users': ok({ users: [], total: 0, limit: 50, offset: 0 }),
    })
    await adminApi.users({ query: 'ada', status: 'active', limit: 50, offset: 100 })
    const search = http.calls[0].url.searchParams
    expect(search.get('query')).toBe('ada')
    expect(search.get('status')).toBe('active')
    expect(search.get('limit')).toBe('50')
    expect(search.get('offset')).toBe('100')

    await adminApi.users({})
    expect(http.calls[1].url.search).toBe('')
  })

  it('sends agent and app filters', async () => {
    const http = mockApi({
      'GET /api/admin/agents': ok({ agents: [] }),
      'GET /api/admin/apps': ok({ apps: [], limit: 200 }),
    })
    await adminApi.agents({ state: 'error', query: 'x' })
    expect(http.calls[0].url.searchParams.get('state')).toBe('error')
    expect(http.calls[0].url.searchParams.get('query')).toBe('x')

    await adminApi.apps({ state: 'running', query: 'mix', limit: 200 })
    expect(http.calls[1].url.searchParams.get('state')).toBe('running')
    expect(http.calls[1].url.searchParams.get('limit')).toBe('200')
  })

  it('posts status changes, retries and shared MCP choices', async () => {
    const http = mockApi({
      'POST /api/admin/users/u_1/status': ok({ user: makeUser({ status: 'disabled' }) }),
      'POST /api/admin/agents/a_1/retry': ok({ agent: {} }),
      'PUT /api/admin/mcp/shared': ok({ ids: [] }),
      'POST /api/admin/mcp/sync': ok({ synced: true }),
      'GET /api/admin/overview': ok({}),
      'GET /api/admin/mcp': ok({ provisioned: false, services: [] }),
      'GET /api/admin/settings': ok({ settings: {} }),
      'PUT /api/admin/settings': ok({ settings: {} }),
      'POST /api/admin/settings/test': ok({ linx: { ok: true } }),
    })

    await adminApi.setUserStatus('u_1', 'disabled')
    await adminApi.retryAgent('a_1')
    await adminApi.setSharedMcp(['m1'])
    await adminApi.syncMcp()
    await adminApi.overview()
    await adminApi.mcp()
    await adminApi.settings()
    await adminApi.updateSettings({ registration: { open: true } })
    await adminApi.testSettings({ linx: { api_key: { clear: true } } })

    expect(http.calls[0].body).toEqual({ status: 'disabled' })
    expect(http.calls[2].body).toEqual({ ids: ['m1'] })
    expect(http.calls[7].body).toEqual({ registration: { open: true } })
    expect(http.calls[8].body).toEqual({ linx: { api_key: { clear: true } } })
    expect(
      http.calls.map((call) => `${call.method} ${call.path}`),
    ).toEqual([
      'POST /api/admin/users/u_1/status',
      'POST /api/admin/agents/a_1/retry',
      'PUT /api/admin/mcp/shared',
      'POST /api/admin/mcp/sync',
      'GET /api/admin/overview',
      'GET /api/admin/mcp',
      'GET /api/admin/settings',
      'PUT /api/admin/settings',
      'POST /api/admin/settings/test',
    ])
  })
})

describe('voice endpoints', () => {
  it('builds the voice URLs', () => {
    expect(voice.base('dev_1')).toBe('/voice/dev_1')
    expect(voice.eventsURL('a b')).toBe('/voice/a%20b/events')
    expect(voice.audioURL('dev_1')).toBe('/voice/dev_1/audio?codec=opus')
    expect(voice.playbackURL('dev_1')).toBe('/voice/dev_1/playback')
  })

  it('posts control commands and reads status', async () => {
    const http = mockApi({
      'GET /voice/dev_1/status': ok({ state: 'listening', connected: true, volume: 80 }),
      'POST /voice/dev_1/pause': ok({}),
      'POST /voice/dev_1/resume': ok({}),
      'POST /voice/dev_1/abort': ok({}),
    })
    expect((await voice.status('dev_1')).volume).toBe(80)
    await voice.pause('dev_1')
    await voice.resume('dev_1')
    await voice.abort('dev_1')
    expect(http.calls.map((call) => `${call.method} ${call.path}`)).toEqual([
      'GET /voice/dev_1/status',
      'POST /voice/dev_1/pause',
      'POST /voice/dev_1/resume',
      'POST /voice/dev_1/abort',
    ])
  })
})
