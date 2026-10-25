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
  Tag,
} from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
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
  render_url?: string
  casting_url?: string
  render_token?: string
  casting_token?: string
  clear_render_token?: boolean
  clear_casting_token?: boolean
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
  if (linxChanged) patch.linx = linx

  const services: NonNullable<PlatformSettingsPatch['services']> = {}
  let servicesChanged = false
  if (trimmed(values.render_url) !== baseline.services.render_url) {
    services.render_url = trimmed(values.render_url)
    servicesChanged = true
  }
  if (trimmed(values.casting_url) !== baseline.services.casting_url) {
    services.casting_url = trimmed(values.casting_url)
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
  const castingToken = secretPatch(
    values.casting_token,
    values.clear_casting_token,
    baseline.services.casting_token.configured,
  )
  if (castingToken) {
    services.casting_token = castingToken
    servicesChanged = true
  }
  if (servicesChanged) patch.services = services

  if (Boolean(values.registration_open) !== baseline.registration.open) {
    patch.registration = { open: Boolean(values.registration_open) }
  }
  return patch
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
      render_url: settingsData.services.render_url,
      casting_url: settingsData.services.casting_url,
      render_token: '',
      casting_token: '',
      clear_render_token: false,
      clear_casting_token: false,
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
      message.success('平台设置已保存，新构建和新开工会立即使用新值')
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
      message[result.linx.ok && result.render.ok && result.casting.ok ? 'success' : 'warning'](
        '连接测试完成',
      )
    },
    onError: (err) => {
      setError(err instanceof ApiError ? err.message : '连接测试失败')
    },
  })

  const runSave = async () => {
    if (!settingsData) return
    const values = await form.validateFields()
    save.mutate(toPatch(values, settingsData))
  }

  const runTest = async () => {
    if (!settingsData) return
    const values = await form.validateFields()
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
            testResult.linx.ok && testResult.render.ok && testResult.casting.ok
              ? 'success'
              : 'warning'
          }
          showIcon
          className="panel-alert"
          message="连接测试"
          description={
            <span>
              Linx：{checkLine(testResult.linx)} · Render：{checkLine(testResult.render)} ·
              Casting：{checkLine(testResult.casting)}
              {testResult.casting.ok ? ` · 发现 ${testResult.screens} 块屏幕` : ''}
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
            <Form.Item label="Base URL" name="agentapi_base_url">
              <Input placeholder="https://agent.example.com" autoComplete="off" />
            </Form.Item>
            <Form.Item
              label="API Key"
              name="agentapi_api_key"
              extra={
                settingsData.agentapi.api_key.configured
                  ? '已保存 API Key；留空保持不变。'
                  : '未保存 API Key。'
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
            <Form.Item label="Agent ID" name="agentapi_agent_id">
              <Input autoComplete="off" />
            </Form.Item>
            <Form.Item label="Agent 版本" name="agentapi_agent_version">
              <InputNumber min={0} style={{ width: 180 }} />
            </Form.Item>
            <Form.Item label="环境 ID" name="agentapi_environment_id">
              <Input autoComplete="off" />
            </Form.Item>
          </section>

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
            <Form.Item label="API Base" name="linx_api_base">
              <Input placeholder="https://xrobo.qiniu.com" autoComplete="off" />
            </Form.Item>
            <Form.Item label="OTA 地址" name="linx_ota_url">
              <Input placeholder="https://xrobo.qiniu.com/ota" autoComplete="off" />
            </Form.Item>
            <Form.Item
              label="API Key"
              name="linx_api_key"
              extra={
                settingsData.linx.api_key.configured
                  ? '已保存 API Key；留空保持不变。修改地址或 Key 后，下一次开工会重建 MCP Server。'
                  : '未保存 API Key。'
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
                <h2>默认屏幕服务</h2>
                <p>平台托管用户的 Render 与 Casting。仍有应用占用这些服务时不能修改。</p>
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
            <Form.Item label="Casting 地址" name="casting_url">
              <Input placeholder="https://casting.example.com" autoComplete="off" />
            </Form.Item>
            <Form.Item
              label="Casting Token"
              name="casting_token"
              extra={
                settingsData.services.casting_token.configured
                  ? '已保存 Token；留空保持不变。'
                  : '可选。'
              }
            >
              <Input.Password
                placeholder={
                  settingsData.services.casting_token.configured ? '已保存，留空保持不变' : '可选'
                }
                autoComplete="new-password"
              />
            </Form.Item>
            {settingsData.services.casting_token.configured ? (
              <Form.Item name="clear_casting_token" valuePropName="checked">
                <Checkbox>清除已保存的 Casting Token</Checkbox>
              </Form.Item>
            ) : null}
          </section>

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
