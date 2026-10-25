export type UserRole = 'admin' | 'user'
export type UserStatus = 'active' | 'disabled'

export interface User {
  id: string
  username: string
  role: UserRole
  status: UserStatus
  created_at: string
  last_login_at?: string
}

export interface VoiceState {
  state: string
  connected: boolean
  session_id?: string
  error?: string
}

export type DeviceState = 'provisioning' | 'registered' | 'error' | 'disabled'

export interface Device {
  id: string
  kind: 'virtual' | 'physical'
  mac: string
  client_id: string
  agent_id: string
  agent_name: string
  state: DeviceState
  step: string
  error?: string
  error_code?: string
  created_at: string
  updated_at: string
  registered_at?: string
  voice: VoiceState
}

export type AppState = 'building' | 'idle' | 'running' | 'error'

export interface AppError {
  stage: string
  code: string
  message?: string
  last_agent_message?: string
}

export interface AppSummary {
  app_id: string
  device_id: string
  name?: string
  title?: string
  summary?: string
  state: AppState
  progress?: string
  build_started_at?: string
  built_at?: string
  build_duration_seconds?: number
  operation_count: number
  screen_id?: string
  started_at?: string
  stopped_at?: string
  error?: AppError
}

export interface Operation {
  operation_id: string
  method: string
  path: string
  summary?: string
  description?: string
  parameters?: unknown[]
  request_body?: unknown
}

export interface AppDetail extends AppSummary {
  message?: string
  uptime_seconds?: number
  operations: Operation[]
  openapi_available: boolean
}

export interface MeResponse {
  user: User
  devices: Device[]
}

export interface RegisterResponse {
  user: User
  device: Device
}

export type ServiceMode = 'managed' | 'custom'

export interface ServiceBindingState {
  mode: ServiceMode
  ready: boolean
  managed: {
    available: boolean
    render_url: string
    casting_url: string
  }
  custom: {
    render_url: string
    casting_url: string
    render_token_configured: boolean
    casting_token_configured: boolean
  }
}

export interface ServiceBindingInput {
  mode: ServiceMode
  render_url?: string
  casting_url?: string
  render_token?: string
  casting_token?: string
  clear_render_token?: boolean
  clear_casting_token?: boolean
}

export interface ServiceCheck {
  ok: boolean
  message?: string
}

export interface ServiceTestResult {
  render: ServiceCheck
  casting: ServiceCheck
  screens: number
}

export interface AdminAgent {
  id: string
  user_id: string
  username?: string
  kind: 'virtual' | 'physical'
  mac: string
  agent_id?: string
  agent_name?: string
  state: DeviceState
  step?: string
  error?: string
  error_code?: string
  created_at: string
  updated_at: string
  registered_at?: string
}

export interface AdminUser {
  id: string
  username: string
  role: UserRole
  status: UserStatus
  created_at: string
  updated_at: string
  last_login_at?: string
  agents: AdminAgent[]
  app_count: number
}

export interface AdminApp {
  app_id: string
  name?: string
  title?: string
  state: AppState
  build_started_at?: string
  built_at?: string
  build_duration_seconds?: number
  operation_count: number
  user_id?: string
  username?: string
  device_id?: string
  agent_id?: string
}

export interface AdminOverview {
  users: { total: number; active: number; disabled: number; admins: number }
  agents: { total: number; registered: number; provisioning: number; error: number; disabled: number }
  apps: { total: number; running: number; building: number; error: number; idle: number }
  platform: { agentapi: boolean; linx: boolean; services: boolean; registration_open: boolean }
}

export interface SecretState {
  configured: boolean
  updated_at?: string
}

export interface PlatformSettings {
  agentapi: {
    base_url: string
    agent_id: string
    agent_version: number
    environment_id: string
    api_key: SecretState
  }
  linx: {
    api_base: string
    ota_url: string
    api_key: SecretState
  }
  services: {
    render_url: string
    casting_url: string
    render_token: SecretState
    casting_token: SecretState
  }
  registration: { open: boolean }
  ready: { agentapi: boolean; linx: boolean; services: boolean }
}

export interface SecretPatch {
  value?: string
  clear?: boolean
}

export interface PlatformSettingsPatch {
  agentapi?: {
    base_url?: string
    api_key?: SecretPatch
    agent_id?: string
    agent_version?: number
    environment_id?: string
  }
  linx?: {
    api_base?: string
    ota_url?: string
    api_key?: SecretPatch
  }
  services?: {
    render_url?: string
    casting_url?: string
    render_token?: SecretPatch
    casting_token?: SecretPatch
  }
  registration?: { open?: boolean }
}

export interface SettingsTestResult {
  linx: ServiceCheck
  render: ServiceCheck
  casting: ServiceCheck
  screens: number
}

export type ScreenStatus = 'free' | 'showing_app' | 'external' | 'unavailable'

export interface ScreenAppRef {
  app_id: string
  name: string
}

export interface Screen {
  screen_id: string
  name: string
  online: boolean
  status: ScreenStatus
  current_app?: ScreenAppRef
}

export interface StopScreenResult {
  screen_id: string
  app: {
    app_id: string
    state: AppState
  }
}

export interface VoiceStatus {
  state: string
  mode?: string
  session_id?: string
  mac?: string
  connected: boolean
  volume: number
  registered: boolean
  activation_code?: string
}

export interface VoiceEvent {
  type: string
  state?: string
  text?: string
  is_aborted?: boolean
  raw?: unknown
}

export class ApiError extends Error {
  status: number
  code: string
  // Session probes and other requests should send the browser to the login
  // page when they return 401. Login and registration failures must stay on
  // the form so the user can see why.
  authRequest: boolean

  constructor(status: number, code: string, message: string, authRequest = false) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.authRequest = authRequest
  }
}

const BASE = '/api/portal'
const ADMIN_BASE = '/api/admin'

async function requestURL<T>(url: string, init: RequestInit = {}): Promise<T> {
  const method = (init.method ?? 'GET').toUpperCase()
  const headers = new Headers(init.headers)
  // The gateway rejects state-changing requests without this content type.
  // Every non-read request gets it, including POSTs that carry no payload.
  if (method !== 'GET' && method !== 'HEAD') {
    headers.set('Content-Type', 'application/json')
  }
  const response = await fetch(url, {
    credentials: 'same-origin',
    ...init,
    headers,
  })
  if (response.status === 204) {
    return undefined as T
  }
  const text = await response.text()
  let body: unknown = undefined
  if (text) {
    try {
      body = JSON.parse(text)
    } catch {
      body = undefined
    }
  }
  if (!response.ok) {
    const detail = (body as { error?: { code?: string; message?: string } } | undefined)?.error
    const authRequest = /\/(login|register)$/.test(new URL(url, window.location.origin).pathname)
    throw new ApiError(
      response.status,
      detail?.code || (response.status === 401 ? 'UNAUTHORIZED' : 'REQUEST_FAILED'),
      detail?.message || text || `请求失败（HTTP ${response.status}）`,
      authRequest,
    )
  }
  return body as T
}

function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  return requestURL<T>(BASE + path, init)
}

function adminRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
  return requestURL<T>(ADMIN_BASE + path, init)
}

function postJSON<T>(path: string, body: unknown = {}): Promise<T> {
  return request<T>(path, { method: 'POST', body: JSON.stringify(body) })
}

function postJSONURL<T>(url: string, body: unknown = {}): Promise<T> {
  return requestURL<T>(url, { method: 'POST', body: JSON.stringify(body) })
}

function voiceBase(deviceId: string): string {
  return `/voice/${encodeURIComponent(deviceId)}`
}

export const api = {
  me: () => request<MeResponse>('/me'),
  register: (username: string, password: string) =>
    postJSON<RegisterResponse>('/register', { username, password }),
  login: (username: string, password: string) =>
    postJSON<{ user: User }>('/login', { username, password }),
  logout: () => postJSON<void>('/logout', {}),
  createDevice: () => postJSON<{ device: Device }>('/devices'),
  getDevice: (id: string) => request<{ device: Device }>(`/devices/${encodeURIComponent(id)}`),
  retryDevice: (id: string) =>
    postJSON<{ device: Device }>(`/devices/${encodeURIComponent(id)}/retry`),
  disconnectDevice: (id: string) =>
    postJSON<{ device: Device }>(`/devices/${encodeURIComponent(id)}/disconnect`),
  reconnectDevice: (id: string) =>
    postJSON<{ device: Device }>(`/devices/${encodeURIComponent(id)}/reconnect`),
  services: () => request<ServiceBindingState>('/services'),
  updateServices: (input: ServiceBindingInput) =>
    request<ServiceBindingState>('/services', { method: 'PUT', body: JSON.stringify(input) }),
  testServices: (input: ServiceBindingInput) =>
    postJSON<ServiceTestResult>('/services/test', input),
  listApps: () => request<{ apps: AppSummary[] }>('/apps'),
  getApp: (id: string) => request<{ app: AppDetail }>(`/apps/${encodeURIComponent(id)}`),
  deleteApp: (id: string) =>
    request<void>(`/apps/${encodeURIComponent(id)}`, {
      method: 'DELETE',
      body: '{}',
    }),
  listScreens: () => request<{ screens: Screen[] }>('/screens'),
  stopScreen: (screenId: string) =>
    postJSON<StopScreenResult>(`/screens/${encodeURIComponent(screenId)}/stop`, {}),
  openapi: async (id: string) => {
    const response = await fetch(`${BASE}/apps/${encodeURIComponent(id)}/openapi.yaml`, {
      credentials: 'same-origin',
    })
    if (!response.ok) {
      let message = `暂时无法读取接口文档（HTTP ${response.status}）`
      try {
        const body = await response.json()
        message = body?.error?.message || message
      } catch {
        // keep the default message
      }
      throw new ApiError(response.status, 'OPENAPI_UNAVAILABLE', message)
    }
    return response.text()
  },
  openapiDownloadURL: (id: string) =>
    `${BASE}/apps/${encodeURIComponent(id)}/openapi.yaml?download=1`,
}

export const adminApi = {
  overview: () => adminRequest<AdminOverview>('/overview'),
  users: (params: { query?: string; status?: string; limit?: number; offset?: number }) => {
    const search = new URLSearchParams()
    if (params.query) search.set('query', params.query)
    if (params.status) search.set('status', params.status)
    if (params.limit) search.set('limit', String(params.limit))
    if (params.offset) search.set('offset', String(params.offset))
    const suffix = search.toString()
    return adminRequest<{ users: AdminUser[]; total: number; limit: number; offset: number }>(
      `/users${suffix ? `?${suffix}` : ''}`,
    )
  },
  user: (id: string) =>
    adminRequest<{ user: AdminUser; apps: AdminApp[] }>(`/users/${encodeURIComponent(id)}`),
  setUserStatus: (id: string, status: UserStatus) =>
    adminRequest<{ user: User }>(`/users/${encodeURIComponent(id)}/status`, {
      method: 'POST',
      body: JSON.stringify({ status }),
    }),
  agents: (params: { state?: string; query?: string }) => {
    const search = new URLSearchParams()
    if (params.state) search.set('state', params.state)
    if (params.query) search.set('query', params.query)
    const suffix = search.toString()
    return adminRequest<{ agents: AdminAgent[] }>(`/agents${suffix ? `?${suffix}` : ''}`)
  },
  retryAgent: (id: string) =>
    adminRequest<{ agent: AdminAgent }>(`/agents/${encodeURIComponent(id)}/retry`, {
      method: 'POST',
      body: '{}',
    }),
  apps: (params: { state?: string; query?: string; limit?: number }) => {
    const search = new URLSearchParams()
    if (params.state) search.set('state', params.state)
    if (params.query) search.set('query', params.query)
    if (params.limit) search.set('limit', String(params.limit))
    const suffix = search.toString()
    return adminRequest<{ apps: AdminApp[]; limit: number }>(`/apps${suffix ? `?${suffix}` : ''}`)
  },
  settings: () => adminRequest<{ settings: PlatformSettings }>('/settings'),
  updateSettings: (patch: PlatformSettingsPatch) =>
    adminRequest<{ settings: PlatformSettings }>('/settings', {
      method: 'PUT',
      body: JSON.stringify(patch),
    }),
  testSettings: (patch: PlatformSettingsPatch) =>
    adminRequest<SettingsTestResult>('/settings/test', {
      method: 'POST',
      body: JSON.stringify(patch),
    }),
}

export const voice = {
  base: voiceBase,
  status: (deviceId: string) => requestURL<VoiceStatus>(`${voiceBase(deviceId)}/status`),
  pause: (deviceId: string) => postJSONURL<unknown>(`${voiceBase(deviceId)}/pause`),
  resume: (deviceId: string) => postJSONURL<unknown>(`${voiceBase(deviceId)}/resume`),
  abort: (deviceId: string) => postJSONURL<unknown>(`${voiceBase(deviceId)}/abort`),
  eventsURL: (deviceId: string) => `${voiceBase(deviceId)}/events`,
  audioURL: (deviceId: string) => `${voiceBase(deviceId)}/audio?codec=opus`,
  playbackURL: (deviceId: string) => `${voiceBase(deviceId)}/playback`,
}
