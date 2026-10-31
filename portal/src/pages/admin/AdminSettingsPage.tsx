import { ApiOutlined, ReloadOutlined } from '@ant-design/icons'
import {
  Alert,
  App as AntApp,
  Button,
  Checkbox,
  Descriptions,
  Form,
  Input,
  InputNumber,
  Space,
  Spin,
  Switch,
  Tabs,
  Tag,
} from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState, type ReactNode } from 'react'
import {
  ApiError,
  adminApi,
  type PlatformSettings,
  type PlatformSettingsPatch,
  type ServiceCheck,
  type SettingsTestResult,
} from '../../api'
import { adminSettingsQueryOptions } from '../../api/queries'
import { usePageTitle } from '../../hooks/usePageTitle'
import { PageHeader } from '../portal/components/PageHeader'
import { AdminMcpPanel } from './AdminMcpPanel'

interface SettingsFormValues {
  agentapi_base_url?: string
  agentapi_api_key?: string
  agentapi_agent_id?: string
  agentapi_agent_version?: number
  agentapi_environment_id?: string
  agentapi_clear_key?: boolean
  linx_api_base?: string
  linx_ota_url?: string
  linx_api_key?: string
  linx_clear_key?: boolean
  linx_public_mcp_url?: string
  linx_mcp_name?: string
  linx_assistant_name?: string
  linx_llm_model_id?: string
  linx_mem_model_id?: string
  linx_intent_model_id?: string
  linx_rotate_mcp_token?: boolean
  render_url?: string
  render_token?: string
  clear_render_token?: boolean
  registration_open?: boolean
}

function secretPatch(value: string | undefined, clear: boolean | undefined, configured: boolean) {
  if (clear) return configured ? { clear: true } : undefined
  const trimmed = value?.trim()
  if (trimmed) return { value: trimmed }
  return undefined
}

function trimmed(value?: string) {
  return (value ?? '').trim()
}

// Only send groups whose values actually changed. The gateway refuses default
// service edits while any app still holds resources, and it drops cached MCP
// resources when Linx credentials change; sending unchanged groups would
// trigger both unnecessarily.
function toPatch(values: SettingsFormValues, baseline: PlatformSettings): PlatformSettingsPatch {
  const patch: PlatformSettingsPatch = {}

  const agentapi: NonNullable<PlatformSettingsPatch['agentapi']> = {}
  let agentapiChanged = false
  if (trimmed(values.agentapi_base_url) !== baseline.agentapi.base_url) {
    agentapi.base_url = trimmed(values.agentapi_base_url)
    agentapiChanged = true
  }
  if (trimmed(values.agentapi_agent_id) !== baseline.agentapi.agent_id) {
    agentapi.agent_id = trimmed(values.agentapi_agent_id)
    agentapiChanged = true
  }
  const agentVersion = Number(values.agentapi_agent_version ?? 0)
  if (agentVersion !== baseline.agentapi.agent_version) {
    agentapi.agent_version = agentVersion
    agentapiChanged = true
  }
  if (trimmed(values.agentapi_environment_id) !== baseline.agentapi.environment_id) {
    agentapi.environment_id = trimmed(values.agentapi_environment_id)
    agentapiChanged = true
  }
  const agentKey = secretPatch(
    values.agentapi_api_key,
    values.agentapi_clear_key,
    baseline.agentapi.api_key.configured,
  )
  if (agentKey) {
    agentapi.api_key = agentKey
    agentapiChanged = true
  }
  if (agentapiChanged) patch.agentapi = agentapi

  const linx: NonNullable<PlatformSettingsPatch['linx']> = {}
  let linxChanged = false
  if (trimmed(values.linx_api_base) !== baseline.linx.api_base) {
    linx.api_base = trimmed(values.linx_api_base)
    linxChanged = true
  }
  if (trimmed(values.linx_ota_url) !== baseline.linx.ota_url) {
    linx.ota_url = trimmed(values.linx_ota_url)
    linxChanged = true
  }
  const linxKey = secretPatch(
    values.linx_api_key,
    values.linx_clear_key,
    baseline.linx.api_key.configured,
  )
  if (linxKey) {
    linx.api_key = linxKey
    linxChanged = true
  }
  for (const [field, formValue, current] of [
    ['public_mcp_url', values.linx_public_mcp_url, baseline.linx.public_mcp_url],
    ['mcp_name', values.linx_mcp_name, baseline.linx.mcp_name],
    ['assistant_name', values.linx_assistant_name, baseline.linx.assistant_name],
    ['llm_model_id', values.linx_llm_model_id, baseline.linx.llm_model_id],
    ['mem_model_id', values.linx_mem_model_id, baseline.linx.mem_model_id],
    ['intent_model_id', values.linx_intent_model_id, baseline.linx.intent_model_id],
  ] as const) {
    if (trimmed(formValue) !== current) {
      linx[field] = trimmed(formValue)
      linxChanged = true
    }
  }
  if (values.linx_rotate_mcp_token) {
    linx.rotate_mcp_token = true
    linxChanged = true
  }
  if (linxChanged) patch.linx = linx

  const services: NonNullable<PlatformSettingsPatch['services']> = {}
  let servicesChanged = false
  if (trimmed(values.render_url) !== baseline.services.render_url) {
    services.render_url = trimmed(values.render_url)
    servicesChanged = true
  }
  const renderToken = secretPatch(
    values.render_token,
    values.clear_render_token,
    baseline.services.render_token.configured,
  )
  if (renderToken) {
    services.render_token = renderToken
    servicesChanged = true
  }
  if (servicesChanged) patch.services = services

  if (Boolean(values.registration_open) !== baseline.registration.open) {
    patch.registration = { open: Boolean(values.registration_open) }
  }
  return patch
}

function Source({ children, href }: { children: ReactNode; href?: string }) {
  return (
    <span>
      {children}
      {href ? (
        <>
          {' '}
          <a href={href} target="_blank" rel="noreferrer">
            打开
          </a>
        </>
      ) : null}
    </span>
  )
}

function checkLine(check: ServiceCheck): string {
  return check.ok ? '可用' : check.message || '不可用'
}

function statusTag(ready: boolean, label = '已配置') {
  return ready ? <Tag color="green">{label}</Tag> : <Tag color="orange">未配置</Tag>
}

export function AdminSettingsPage() {
  usePageTitle('平台设置')
  const [form] = Form.useForm<SettingsFormValues>()
  const [error, setError] = useState<string>()
  const [testResult, setTestResult] = useState<SettingsTestResult>()
  const queryClient = useQueryClient()
  const { message } = AntApp.useApp()
  const settings = useQuery(adminSettingsQueryOptions)
  const settingsData = settings.data?.settings

  useEffect(() => {
    if (!settingsData) return
    form.setFieldsValue({
      agentapi_base_url: settingsData.agentapi.base_url,
      agentapi_agent_id: settingsData.agentapi.agent_id,
      agentapi_agent_version: settingsData.agentapi.agent_version,
      agentapi_environment_id: settingsData.agentapi.environment_id,
      agentapi_api_key: '',
      agentapi_clear_key: false,
      linx_api_base: settingsData.linx.api_base,
      linx_ota_url: settingsData.linx.ota_url,
      linx_api_key: '',
      linx_clear_key: false,
      linx_public_mcp_url: settingsData.linx.public_mcp_url,
      linx_mcp_name: settingsData.linx.mcp_name,
      linx_assistant_name: settingsData.linx.assistant_name,
      linx_llm_model_id: settingsData.linx.llm_model_id,
      linx_mem_model_id: settingsData.linx.mem_model_id,
      linx_intent_model_id: settingsData.linx.intent_model_id,
      linx_rotate_mcp_token: false,
      render_url: settingsData.services.render_url,
      render_token: '',
      clear_render_token: false,
      registration_open: settingsData.registration.open,
    })
  }, [settingsData, form])

  const save = useMutation({
    mutationFn: (patch: PlatformSettingsPatch) => adminApi.updateSettings(patch),
    onSuccess: (data) => {
      setError(undefined)
      setTestResult(undefined)
      queryClient.setQueryData(adminSettingsQueryOptions.queryKey, data)
      queryClient.invalidateQueries({ queryKey: ['admin', 'settings'] })
      queryClient.invalidateQueries({ queryKey: ['admin', 'overview'] })
      message.success('平台设置已保存，新构建和新开通会立即使用新值')
      if (data.warning) message.warning(data.warning, 8)
    },
    onError: (err) => {
      setError(err instanceof ApiError ? err.message : '保存平台设置失败')
    },
  })

  const test = useMutation({
    mutationFn: (patch: PlatformSettingsPatch) => adminApi.testSettings(patch),
    onSuccess: (result) => {
      setError(undefined)
      setTestResult(result)
      message[result.linx.ok && result.render.ok ? 'success' : 'warning'](
        '连接测试完成',
      )
    },
    onError: (err) => {
      setError(err instanceof ApiError ? err.message : '连接测试失败')
    },
  })

  const runSave = async () => {
    if (!settingsData) return
    const values = await form.validateFields().catch(() => undefined)
    if (!values) return
    save.mutate(toPatch(values, settingsData))
  }

  const runTest = async () => {
    if (!settingsData) return
    const values = await form.validateFields().catch(() => undefined)
    if (!values) return
    test.mutate(toPatch(values, settingsData))
  }

  return (
    <div className="page">
      <PageHeader
        eyebrow="Admin"
        title="平台设置"
        description="平台级 AgentAPI、Linx 与默认屏幕服务。密钥保存后不回显。"
        actions={
          <Button
            icon={<ReloadOutlined />}
            loading={settings.isFetching}
            onClick={() => settings.refetch()}
          >
            刷新
          </Button>
        }
      />

      {error ? (
        <Alert
          type="error"
          showIcon
          closable
          className="panel-alert"
          message={error}
          onClose={() => setError(undefined)}
        />
      ) : null}

      {testResult ? (
        <Alert
          type={
            testResult.linx.ok && testResult.render.ok
              ? 'success'
              : 'warning'
          }
          showIcon
          className="panel-alert"
          message="连接测试"
          description={
            <span>
              Linx：{checkLine(testResult.linx)} · Render：{checkLine(testResult.render)}
            </span>
          }
        />
      ) : null}

      {settings.isPending ? (
        <section className="surface">
          <div style={{ padding: 32, textAlign: 'center' }}>
            <Spin />
          </div>
        </section>
      ) : settings.isError ? (
        <Alert
          type="error"
          showIcon
          message="无法读取平台设置"
          description={
            settings.error instanceof ApiError ? settings.error.message : String(settings.error)
          }
        />
      ) : settingsData ? (
        <Form form={form} layout="vertical" requiredMark={false}>
          <Tabs
            className="settings-tabs"
            defaultActiveKey="agentapi"
            items={[
              {
                key: 'agentapi',
                label: '构建（AgentAPI）',
                forceRender: true,
                children: (
                  <>
                  <section className="surface settings-section">
                    <div className="section-header">
                      <div>
                        <h2>AgentAPI</h2>
                        <p>用于云端编码智能体的构建。修改后新构建立即生效。</p>
                      </div>
                      {statusTag(settingsData.ready.agentapi)}
                    </div>
                    <Descriptions
                      size="small"
                      column={1}
                      items={[
                        {
                          key: 'key',
                          label: 'API Key',
                          children: settingsData.agentapi.api_key.configured ? (
                            <Tag color="green">已配置</Tag>
                          ) : (
                            <Tag color="orange">未配置</Tag>
                          ),
                        },
                      ]}
                    />
                    <Form.Item
                      label="Base URL"
                      name="agentapi_base_url"
                      extra={
                        <Source href="https://portal.qiniu.com/agentapi">
                          AgentAPI 服务地址：华东 https://agent.qiniuapi.com，美西 https://agent.sufy.com。
                          资源按区域隔离，地址要与下面的 Agent、环境同一区域。控制台：
                        </Source>
                      }
                    >
                      <Input placeholder="https://agent.qiniuapi.com" autoComplete="off" />
                    </Form.Item>
                    <Form.Item
                      label="API Key"
                      name="agentapi_api_key"
                      extra={
                        <Source href="https://portal.qiniu.com/agentapi">
                          {settingsData.agentapi.api_key.configured
                            ? '已保存 API Key；留空保持不变。'
                            : '未保存 API Key。'}
                          在七牛控制台创建 MaaS API Key，会自动绑定到 AgentAPI 账户。
                        </Source>
                      }
                    >
                      <Input.Password
                        placeholder={
                          settingsData.agentapi.api_key.configured ? '已保存，留空保持不变' : '输入 API Key'
                        }
                        autoComplete="new-password"
                      />
                    </Form.Item>
                    {settingsData.agentapi.api_key.configured ? (
                      <Form.Item name="agentapi_clear_key" valuePropName="checked">
                        <Checkbox>清除已保存的 AgentAPI API Key</Checkbox>
                      </Form.Item>
                    ) : null}
                    <Form.Item
                      label="Agent ID"
                      name="agentapi_agent_id"
                      extra="负责生成应用的构建智能体 ID（形如 agt_…）。在 AgentAPI 控制台创建 app-builder 智能体后复制。"
                    >
                      <Input autoComplete="off" />
                    </Form.Item>
                    <Form.Item
                      label="Agent 版本"
                      name="agentapi_agent_version"
                      extra="可选。固定使用某个智能体版本；填 0 表示每次使用最新版本。"
                    >
                      <InputNumber min={0} style={{ width: 180 }} />
                    </Form.Item>
                    <Form.Item
                      label="环境 ID"
                      name="agentapi_environment_id"
                      extra="构建会话运行的云端环境 ID（形如 env_…）。在 AgentAPI 控制台的“环境”里创建后复制。"
                    >
                      <Input autoComplete="off" />
                    </Form.Item>
                  </section>
                  </>
                ),
              },
              {
                key: 'linx',
                label: '灵矽平台',
                forceRender: true,
                children: (
                  <>
                  <section className="surface settings-section">
                    <div className="section-header">
                      <div>
                        <h2>Linx</h2>
                        <p>用于创建用户智能体、挂载 MCP 与设备绑定。</p>
                      </div>
                      {statusTag(settingsData.ready.linx)}
                    </div>
                    <Descriptions
                      size="small"
                      column={1}
                      items={[
                        {
                          key: 'key',
                          label: 'API Key',
                          children: settingsData.linx.api_key.configured ? (
                            <Tag color="green">已配置</Tag>
                          ) : (
                            <Tag color="orange">未配置</Tag>
                          ),
                        },
                      ]}
                    />
                    <Form.Item
                      label="API Base"
                      name="linx_api_base"
                      extra="灵矽开放 API 地址，一般保持 https://xrobo.qiniu.com。"
                    >
                      <Input placeholder="https://xrobo.qiniu.com" autoComplete="off" />
                    </Form.Item>
                    <Form.Item
                      label="OTA 地址"
                      name="linx_ota_url"
                      extra="虚拟设备上报并领取激活码、设备 Token 的地址，一般保持 https://xrobo.qiniuapi.com/v1/ota/。"
                    >
                      <Input placeholder="https://xrobo.qiniuapi.com/v1/ota/" autoComplete="off" />
                    </Form.Item>
                    <Form.Item
                      label="API Key"
                      name="linx_api_key"
                      extra={
                        <Source href="https://xrobo.qiniu.com/">
                          {settingsData.linx.api_key.configured
                            ? '已保存 API Key；留空保持不变。修改地址或 Key 后，下一次开通会重建 MCP Server。'
                            : '未保存 API Key。'}
                          在灵矽智控台“API Key 管理”创建，用它为每位用户创建智能体。
                        </Source>
                      }
                    >
                      <Input.Password
                        placeholder={
                          settingsData.linx.api_key.configured ? '已保存，留空保持不变' : '输入 API Key'
                        }
                        autoComplete="new-password"
                      />
                    </Form.Item>
                    {settingsData.linx.api_key.configured ? (
                      <Form.Item name="linx_clear_key" valuePropName="checked">
                        <Checkbox>清除已保存的 Linx API Key</Checkbox>
                      </Form.Item>
                    ) : null}
                  </section>
                  <section className="surface settings-section">
                    <div className="section-header">
                      <div>
                        <h2>新智能体的默认参数</h2>
                        <p>新用户的智能体按下面的参数创建，已创建的智能体不受影响。用户之后可以在自己的“智能体设置”里调整。</p>
                      </div>
                    </div>
                    <Form.Item
                      label="助手名称"
                      name="linx_assistant_name"
                      extra="新建智能体里的助手昵称，对应灵矽 assistantName。"
                    >
                      <Input placeholder="小灵" autoComplete="off" />
                    </Form.Item>
                    <Form.Item
                      label="大模型 ID"
                      name="linx_llm_model_id"
                      extra={
                        <Source href="https://linx.qiniu.com/docs/xrobot/">
                          对应灵矽 llmModelId，默认 DeepSeek V4 Flash。其他可选模型见开放 API
                          GET /v1/agents/models/llm，文档：
                        </Source>
                      }
                    >
                      <Input autoComplete="off" />
                    </Form.Item>
                    <Form.Item
                      label="记忆模型 ID"
                      name="linx_mem_model_id"
                      extra="对应灵矽 memModelId：Memory_nomem 为无记忆，Memory_long_term_memory 为长期记忆。"
                    >
                      <Input autoComplete="off" />
                    </Form.Item>
                    <Form.Item
                      label="意图模型 ID"
                      name="linx_intent_model_id"
                      extra="对应灵矽 intentModelId：Intent_function_call 为统一意图识别（调用 MCP 必须使用它），Intent_nointent 为无意图识别。"
                    >
                      <Input autoComplete="off" />
                    </Form.Item>
                  </section>
                  </>
                ),
              },
              {
                key: 'mcp',
                label: 'MCP 服务',
                forceRender: true,
                children: (
                  <>
                  <section className="surface settings-section">
                    <div className="section-header">
                      <div>
                        <h2>网关 MCP 连接</h2>
                        <p>灵矽云通过这个 MCP 控制应用。地址和 Token 保存后会同步到灵矽。</p>
                      </div>
                      {statusTag(
                        Boolean(settingsData.linx.public_mcp_url) && settingsData.linx.mcp_token.configured,
                      )}
                    </div>
                    <Form.Item
                      label="公网 MCP 地址"
                      name="linx_public_mcp_url"
                      extra="灵矽云访问本网关 MCP 的公网地址，必须以 /mcp 结尾（Streamable HTTP），例如 https://gateway.example.com/mcp。局域网地址灵矽云访问不到。修改后下一次开通会重建 MCP。"
                    >
                      <Input placeholder="https://gateway.example.com/mcp" autoComplete="off" />
                    </Form.Item>
                    <Form.Item
                      label="MCP 名称"
                      name="linx_mcp_name"
                      extra="在灵矽平台创建的 MCP 服务名称，同一个平台账号下需要唯一。"
                    >
                      <Input placeholder="Open Anywhere" autoComplete="off" />
                    </Form.Item>
                    <Form.Item
                      label="MCP Token"
                      extra={
                        settingsData.linx.mcp_token.configured
                          ? '已自动生成并加密保存，灵矽云用它访问本网关 MCP，不会显示明文。'
                          : '首次启动会自动生成。'
                      }
                    >
                      <Form.Item name="linx_rotate_mcp_token" valuePropName="checked" noStyle>
                        <Checkbox>保存时重新生成，并同步到灵矽 MCP（旧 Token 立即失效）</Checkbox>
                      </Form.Item>
                    </Form.Item>
                  </section>
                      <AdminMcpPanel />
                  </>
                ),
              },
              {
                key: 'services',
                label: '屏幕服务',
                forceRender: true,
                children: (
                  <>
                  <section className="surface settings-section">
                    <div className="section-header">
                      <div>
                        <h2>默认屏幕服务</h2>
                        <p>平台托管用户的应用运行服务。仍有应用占用时不能修改。</p>
                      </div>
                      {statusTag(settingsData.ready.services)}
                    </div>
                    <Form.Item label="Render 地址" name="render_url">
                      <Input placeholder="https://render.example.com" autoComplete="off" />
                    </Form.Item>
                    <Form.Item
                      label="Render Token"
                      name="render_token"
                      extra={
                        settingsData.services.render_token.configured
                          ? '已保存 Token；留空保持不变。'
                          : '可选。'
                      }
                    >
                      <Input.Password
                        placeholder={
                          settingsData.services.render_token.configured ? '已保存，留空保持不变' : '可选'
                        }
                        autoComplete="new-password"
                      />
                    </Form.Item>
                    {settingsData.services.render_token.configured ? (
                      <Form.Item name="clear_render_token" valuePropName="checked">
                        <Checkbox>清除已保存的 Render Token</Checkbox>
                      </Form.Item>
                    ) : null}
                  </section>
                  </>
                ),
              },
              {
                key: 'registration',
                label: '注册',
                forceRender: true,
                children: (
                  <>
                  <section className="surface settings-section">
                    <div className="section-header">
                      <div>
                        <h2>注册</h2>
                        <p>关闭时注册接口拒绝新账号；已有账号不受影响。</p>
                      </div>
                      {settingsData.registration.open ? (
                        <Tag color="green">开放</Tag>
                      ) : (
                        <Tag>关闭</Tag>
                      )}
                    </div>
                    <Form.Item name="registration_open" valuePropName="checked">
                      <Switch checkedChildren="开放注册" unCheckedChildren="关闭注册" />
                    </Form.Item>
                    <Alert
                      type="warning"
                      showIcon
                      message="确认租户隔离已验证再开放注册"
                      description="开放后任何人都可以创建账号并获得智能体。"
                    />
                  </section>
                  </>
                ),
              },
            ]}
          />

          <section className="surface settings-section">
            <Space wrap>
              <Button type="primary" loading={save.isPending} onClick={runSave}>
                保存设置
              </Button>
              <Button icon={<ApiOutlined />} loading={test.isPending} onClick={runTest}>
                测试连接
              </Button>
              <span className="muted-text">测试会使用当前表单值，不会保存。</span>
            </Space>
          </section>
        </Form>
      ) : null}
    </div>
  )
}
