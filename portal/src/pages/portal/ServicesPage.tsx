import { ApiOutlined, CloudServerOutlined, ReloadOutlined } from '@ant-design/icons'
import {
  Alert,
  App as AntApp,
  Button,
  Checkbox,
  Descriptions,
  Form,
  Input,
  Radio,
  Space,
  Spin,
  Tag,
} from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Link } from '@tanstack/react-router'
import {
  ApiError,
  api,
  type ServiceBindingInput,
  type ServiceMode,
  type ServiceTestResult,
} from '../../api'
import { servicesQueryOptions } from '../../api/queries'
import { usePageTitle } from '../../hooks/usePageTitle'
import { PageHeader } from './components/PageHeader'

interface FormValues {
  mode: ServiceMode
  render_url?: string
  casting_url?: string
  render_token?: string
  casting_token?: string
  clear_render_token?: boolean
  clear_casting_token?: boolean
}

function toInput(values: FormValues): ServiceBindingInput {
  const input: ServiceBindingInput = { mode: values.mode }
  if (values.mode === 'custom') {
    input.render_url = (values.render_url ?? '').trim()
    input.casting_url = (values.casting_url ?? '').trim()
    if (values.render_token?.trim()) input.render_token = values.render_token.trim()
    if (values.casting_token?.trim()) input.casting_token = values.casting_token.trim()
  }
  if (values.clear_render_token) input.clear_render_token = true
  if (values.clear_casting_token) input.clear_casting_token = true
  return input
}

function checkText(result: ServiceTestResult): string {
  const parts = [
    result.render.ok ? '运行服务可用' : `运行服务：${result.render.message ?? '不可用'}`,
    result.casting.ok ? '投屏服务可用' : `投屏服务：${result.casting.message ?? '不可用'}`,
  ]
  if (result.casting.ok) {
    parts.push(`发现 ${result.screens} 块屏幕`)
  }
  return parts.join(' · ')
}

export function ServicesPage() {
  usePageTitle('服务')
  const [form] = Form.useForm<FormValues>()
  const [error, setError] = useState<string>()
  const [testResult, setTestResult] = useState<ServiceTestResult>()
  const queryClient = useQueryClient()
  const { message } = AntApp.useApp()
  const services = useQuery(servicesQueryOptions)
  const mode = Form.useWatch('mode', form) ?? 'managed'

  useEffect(() => {
    if (!services.data) return
    form.setFieldsValue({
      mode: services.data.mode,
      render_url: services.data.custom.render_url,
      casting_url: services.data.custom.casting_url,
      render_token: '',
      casting_token: '',
      clear_render_token: false,
      clear_casting_token: false,
    })
  }, [services.data, form])

  const save = useMutation({
    mutationFn: (values: FormValues) => api.updateServices(toInput(values)),
    onSuccess: (data) => {
      setError(undefined)
      setTestResult(undefined)
      queryClient.setQueryData(servicesQueryOptions.queryKey, data)
      queryClient.invalidateQueries({ queryKey: ['services'] })
      message.success('连接设置已保存')
    },
    onError: (err) => {
      setError(err instanceof ApiError ? err.message : '暂时无法保存连接设置')
    },
  })

  const test = useMutation({
    mutationFn: (values: FormValues) => api.testServices(toInput(values)),
    onSuccess: (result) => {
      setError(undefined)
      setTestResult(result)
      message[result.render.ok && result.casting.ok ? 'success' : 'warning']('连接测试完成')
    },
    onError: (err) => {
      setError(err instanceof ApiError ? err.message : '暂时无法完成连接测试')
    },
  })

  const submit = async () => {
    // A rejected validation is shown on the form; it must not surface as an
    // unhandled rejection.
    const values = await form
      .validateFields(
        mode === 'custom'
          ? ['mode', 'render_url', 'casting_url', 'render_token', 'casting_token']
          : ['mode'],
      )
      .catch(() => undefined)
    if (!values) return
    save.mutate(values)
  }

  const runTest = async () => {
    const values = await form
      .validateFields(
        mode === 'custom'
          ? ['mode', 'render_url', 'casting_url', 'render_token', 'casting_token']
          : ['mode'],
      )
      .catch(() => undefined)
    if (!values) return
    test.mutate(values)
  }

  const data = services.data
  const managedUnavailable = Boolean(data && !data.managed.available)

  return (
    <div className="page">
      <PageHeader
        eyebrow="屏幕连接"
        title="屏幕连接"
        description="默认使用平台提供的服务。如果你有自己的运行与投屏服务，也可以在这里接入。"
        actions={
          <Button
            icon={<ReloadOutlined />}
            loading={services.isFetching}
            onClick={() => services.refetch()}
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

      {data ? (
        <Alert
          type={data.ready ? 'success' : 'warning'}
          showIcon
          className="panel-alert"
          message={data.ready ? '屏幕连接已就绪' : '屏幕连接还不可用'}
          description={
            data.ready
              ? '应用会通过这套服务出现在屏幕上。'
              : '应用暂时还不能上屏。可以检查下面的设置，或稍后再试。'
          }
        />
      ) : null}

      {testResult ? (
        <Alert
          type={testResult.render.ok && testResult.casting.ok ? 'success' : 'warning'}
          showIcon
          className="panel-alert"
          message="连接测试"
          description={checkText(testResult)}
        />
      ) : null}

      {services.isPending ? (
        <section className="surface">
          <div style={{ padding: 32, textAlign: 'center' }}>
            <Spin />
          </div>
        </section>
      ) : services.isError ? (
        <Alert
          type="error"
          showIcon
          message="暂时无法读取屏幕连接"
          description={
            services.error instanceof ApiError ? services.error.message : String(services.error)
          }
        />
      ) : (
        <Form form={form} layout="vertical" requiredMark={false}>
          <section className="surface settings-section">
            <div className="section-header">
              <div>
                <h2>由谁提供</h2>
                <p>默认使用平台提供的服务；接入自己的服务时，地址需要能从公网访问。</p>
              </div>
            </div>
            <Form.Item name="mode">
              <Radio.Group className="service-mode-group">
                <Radio.Button value="managed" disabled={managedUnavailable}>
                  <CloudServerOutlined /> 平台提供
                </Radio.Button>
                <Radio.Button value="custom">
                  <ApiOutlined /> 自己接入
                </Radio.Button>
              </Radio.Group>
            </Form.Item>

            {mode === 'managed' ? (
              <div className="service-managed">
                <Descriptions
                  size="small"
                  column={{ xs: 1, sm: 2 }}
                  items={[
                    {
                      key: 'status',
                      label: '平台服务',
                      children: data?.managed.available ? (
                        <Tag color="green">已配置</Tag>
                      ) : (
                        <Tag color="orange">未配置</Tag>
                      ),
                    },
                    {
                      key: 'render',
                      label: '运行服务地址',
                      children: data?.managed.render_url ? (
                        <code className="inline-code">{data.managed.render_url}</code>
                      ) : (
                        '—'
                      ),
                    },
                    {
                      key: 'casting',
                      label: '投屏服务地址',
                      children: data?.managed.casting_url ? (
                        <code className="inline-code">{data.managed.casting_url}</code>
                      ) : (
                        '—'
                      ),
                    },
                  ]}
                />
                {!data?.managed.available ? (
                  <Alert
                    type="warning"
                    showIcon
                    message="平台服务还没准备好"
                    description="可以稍后再试，或切换到「自己接入」并填写你的服务地址。"
                  />
                ) : null}
              </div>
            ) : (
              <>
                <Form.Item
                  label="运行服务地址"
                  name="render_url"
                  rules={[{ required: true, message: '请输入运行服务地址' }]}
                >
                  <Input placeholder="https://render.example.com" autoComplete="off" />
                </Form.Item>
                <Form.Item
                  label="运行服务访问令牌"
                  name="render_token"
                  extra={
                    data?.custom.render_token_configured
                      ? '已保存令牌；留空保持不变，勾选下方选项可清除。'
                      : '可选。保存后不会再次显示。'
                  }
                >
                  <Input.Password
                    placeholder={
                      data?.custom.render_token_configured ? '已保存，留空保持不变' : '可选'
                    }
                    autoComplete="new-password"
                  />
                </Form.Item>
                {data?.custom.render_token_configured ? (
                  <Form.Item name="clear_render_token" valuePropName="checked">
                    <Checkbox>清除已保存的运行服务令牌</Checkbox>
                  </Form.Item>
                ) : null}
                <Form.Item
                  label="投屏服务地址"
                  name="casting_url"
                  rules={[{ required: true, message: '请输入投屏服务地址' }]}
                >
                  <Input placeholder="https://casting.example.com" autoComplete="off" />
                </Form.Item>
                <Form.Item
                  label="投屏服务访问令牌"
                  name="casting_token"
                  extra={
                    data?.custom.casting_token_configured
                      ? '已保存令牌；留空保持不变，勾选下方选项可清除。'
                      : '可选。保存后不会再次显示。'
                  }
                >
                  <Input.Password
                    placeholder={
                      data?.custom.casting_token_configured ? '已保存，留空保持不变' : '可选'
                    }
                    autoComplete="new-password"
                  />
                </Form.Item>
                {data?.custom.casting_token_configured ? (
                  <Form.Item name="clear_casting_token" valuePropName="checked">
                    <Checkbox>清除已保存的投屏服务令牌</Checkbox>
                  </Form.Item>
                ) : null}
              </>
            )}

            <Space wrap>
              <Button
                type="primary"
                loading={save.isPending}
                disabled={mode === 'managed' && managedUnavailable}
                onClick={submit}
              >
                保存设置
              </Button>
              <Button loading={test.isPending} onClick={runTest}>
                测试连接
              </Button>
              <Link to="/portal/screens" className="text-link">
                查看屏幕
              </Link>
            </Space>
            <p className="muted-text" style={{ marginTop: 12 }}>
              屏幕正在播放内容时不能修改连接，请先到
              <Link to="/portal/screens"> 屏幕页 </Link>
              停止播放。地址不能指向本机或内网。
            </p>
          </section>
        </Form>
      )}
    </div>
  )
}
