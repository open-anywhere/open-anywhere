import type {
  AdminAgent,
  AdminApp,
  AdminOverview,
  AdminUser,
  AgentSettings,
  AgentOptions,
  AppDetail,
  AppSummary,
  Device,
  McpService,
  PlatformSettings,
  Screen,
  ServiceBindingState,
  User,
} from '../api'

export function makeUser(overrides: Partial<User> = {}): User {
  return {
    id: 'u_1',
    username: 'ada',
    role: 'user',
    status: 'active',
    created_at: '2026-10-01T10:00:00Z',
    last_login_at: '2026-10-02T10:00:00Z',
    ...overrides,
  }
}

export function makeDevice(overrides: Partial<Device> = {}): Device {
  return {
    id: 'dev_1',
    kind: 'virtual',
    mac: 'AA:BB:CC:DD:EE:01',
    client_id: 'client_1',
    agent_id: 'agent_1',
    agent_name: '小灵',
    name: '客厅',
    state: 'registered',
    step: 'done',
    created_at: '2026-10-01T10:00:00Z',
    updated_at: '2026-10-02T10:00:00Z',
    registered_at: '2026-10-01T10:01:00Z',
    voice: { state: 'listening', connected: true, session_id: 's_1' },
    ...overrides,
  }
}

export function makeApp(overrides: Partial<AppSummary> = {}): AppSummary {
  return {
    app_id: 'app_1',
    device_id: 'AA:BB:CC:DD:EE:01',
    name: 'movie-quiz',
    title: '电影知识竞赛',
    summary: '五个人一起玩的电影抢答',
    state: 'idle',
    operation_count: 2,
    built_at: '2026-10-02T10:00:00Z',
    ...overrides,
  }
}

export function makeAppDetail(overrides: Partial<AppDetail> = {}): AppDetail {
  return {
    ...makeApp(),
    openapi_available: true,
    operations: [
      {
        operation_id: 'op_1',
        method: 'post',
        path: '/next-question',
        summary: '出下一题',
      },
      { operation_id: 'op_2', method: 'get', path: '/state' },
    ],
    ...overrides,
  }
}

export function makeScreen(overrides: Partial<Screen> = {}): Screen {
  return {
    screen_id: 'screen_1',
    name: '客厅电视',
    online: true,
    status: 'free',
    ...overrides,
  }
}

export function makeServices(overrides: Partial<ServiceBindingState> = {}): ServiceBindingState {
  return {
    mode: 'managed',
    ready: true,
    managed: {
      available: true,
      render_url: 'https://render.example.com',
      casting_url: 'https://casting.example.com',
    },
    custom: {
      render_url: '',
      casting_url: '',
      render_token_configured: false,
      casting_token_configured: false,
    },
    ...overrides,
  }
}

export function makeMcp(overrides: Partial<McpService> = {}): McpService {
  return {
    id: 'mcp_1',
    name: '天气',
    desc: '查天气',
    private: false,
    required: false,
    shared: false,
    ...overrides,
  }
}

export function makeAdminUser(overrides: Partial<AdminUser> = {}): AdminUser {
  return {
    id: 'u_1',
    username: 'ada',
    role: 'user',
    status: 'active',
    created_at: '2026-10-01T10:00:00Z',
    updated_at: '2026-10-02T10:00:00Z',
    last_login_at: '2026-10-02T10:00:00Z',
    agents: [makeAdminAgent()],
    app_count: 3,
    ...overrides,
  }
}

export function makeAdminAgent(overrides: Partial<AdminAgent> = {}): AdminAgent {
  return {
    id: 'agent_device_1',
    user_id: 'u_1',
    username: 'ada',
    kind: 'virtual',
    mac: 'AA:BB:CC:DD:EE:01',
    agent_id: 'agent_1',
    agent_name: '小灵',
    state: 'registered',
    step: 'done',
    created_at: '2026-10-01T10:00:00Z',
    updated_at: '2026-10-02T10:00:00Z',
    registered_at: '2026-10-01T10:01:00Z',
    ...overrides,
  }
}

export function makeAdminApp(overrides: Partial<AdminApp> = {}): AdminApp {
  return {
    app_id: 'app_1',
    name: 'movie-quiz',
    title: '电影知识竞赛',
    state: 'running',
    built_at: '2026-10-02T10:00:00Z',
    operation_count: 2,
    user_id: 'u_1',
    username: 'ada',
    device_id: 'AA:BB:CC:DD:EE:01',
    agent_id: 'agent_1',
    ...overrides,
  }
}

export function makeAgentSettings(overrides: Partial<AgentSettings> = {}): AgentSettings {
  return {
    assistant_name: '小灵',
    tts_voice_id: 'voice_1',
    lang_code: 'zh-CN',
    llm_model_id: 'llm_1',
    mem_model_id: 'memory_long_term_memory',
    voice_volume: 60,
    voice_speed: 1,
    llm_temperature: 0.7,
    llm_max_tokens: 500,
    idle_timeout_seconds: 120,
    disable_greeting: false,
    system_prompt: '你是小灵。',
    chat_history: 1,
    ...overrides,
  }
}

export function makeAgentOptions(overrides: Partial<AgentOptions> = {}): AgentOptions {
  return {
    voices: [
      {
        id: 'voice_1',
        name: '晴川',
        gender: 'female',
        styles: ['温柔', 'wenrou'],
        languages: ['zh-CN'],
        tier: 'basic',
      },
      { id: 'voice_2', name: '凌云', gender: 'male', tier: 'pro' },
    ],
    languages: [{ id: 'zh-CN', name: '中文' }],
    llm_models: [{ id: 'llm_1', name: '大模型一', comment: '通用' }],
    memory_modes: [
      { id: 'memory_long_term_memory', name: '长期记忆' },
      { id: 'memory_nomem', name: '无记忆' },
    ],
    default_system_prompt: '默认角色介绍',
    ...overrides,
  }
}

export function makeAdminOverview(overrides: Partial<AdminOverview> = {}): AdminOverview {
  return {
    users: { total: 12, active: 10, disabled: 2, admins: 1 },
    agents: { total: 10, registered: 8, provisioning: 1, error: 1, disabled: 0 },
    apps: { total: 7, running: 2, building: 1, error: 0, idle: 4 },
    platform: {
      agentapi: true,
      linx: true,
      mcp: true,
      services: true,
      registration_open: false,
    },
    ...overrides,
  }
}

export function makePlatformSettings(overrides: Partial<PlatformSettings> = {}): PlatformSettings {
  return {
    agentapi: {
      base_url: 'https://agent.qiniuapi.com',
      agent_id: 'agt_1',
      agent_version: 0,
      environment_id: 'env_1',
      api_key: { configured: true, updated_at: '2026-10-01T10:00:00Z' },
    },
    linx: {
      api_base: 'https://xrobo.qiniu.com',
      ota_url: 'https://xrobo.qiniuapi.com/v1/ota/',
      api_key: { configured: true },
      public_mcp_url: 'https://gateway.example.com/mcp',
      mcp_name: 'Open Anywhere',
      mcp_token: { configured: true },
      assistant_name: '小灵',
      llm_model_id: 'llm_1',
      mem_model_id: 'Memory_long_term_memory',
      intent_model_id: 'Intent_function_call',
    },
    services: {
      render_url: 'https://render.example.com',
      casting_url: 'https://casting.example.com',
      render_token: { configured: false },
      casting_token: { configured: false },
    },
    registration: { open: false },
    ready: { agentapi: true, linx: true, mcp: true, services: true },
    ...overrides,
  }
}
