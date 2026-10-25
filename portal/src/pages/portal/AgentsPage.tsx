import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { Alert, App as AntApp, Button, Space } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { ApiError, api, type CreateDeviceInput } from '../../api'
import { appsQueryOptions, meQueryOptions } from '../../api/queries'
import { useDeviceConnection } from '../../hooks/useDeviceConnection'
import { usePageTitle } from '../../hooks/usePageTitle'
import { deviceForApp } from '../../lib/device'
import { AddDeviceModal } from './components/AddDeviceModal'
import { AppRow } from './components/AppRow'
import { DevicePanel } from './components/DevicePanel'
import { PageHeader } from './components/PageHeader'

export function AgentsPage() {
  usePageTitle('智能体')
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { message, modal } = AntApp.useApp()

  const me = useQuery({
    ...meQueryOptions,
    refetchInterval: (query) =>
      query.state.data?.devices.some((device) => device.state === 'provisioning') ? 2_000 : false,
  })

  const [addOpen, setAddOpen] = useState(false)

  const createDevice = useMutation({
    mutationFn: (input?: CreateDeviceInput) => api.createDevice(input),
    onSuccess: (_, input) => {
      setAddOpen(false)
      message.success(input?.kind === 'physical' ? '设备已绑定' : '正在准备你的智能体')
      queryClient.invalidateQueries({ queryKey: ['me'] })
      queryClient.invalidateQueries({ queryKey: ['apps'] })
    },
    onError: (error) => {
      message.error(error instanceof ApiError ? error.message : '暂时无法添加设备')
    },
  })

  const removeDevice = useMutation({
    mutationFn: ({ id, force }: { id: string; force?: boolean }) => api.deleteDevice(id, force),
    onSuccess: () => {
      message.success('设备已移除')
      queryClient.invalidateQueries({ queryKey: ['me'] })
      queryClient.invalidateQueries({ queryKey: ['apps'] })
    },
    onError: (error, variables) => {
      const text = error instanceof ApiError ? error.message : '移除失败'
      if (error instanceof ApiError && error.code === 'UNBIND_FAILED' && !variables.force) {
        modal.confirm({
          title: '向灵矽解绑失败',
          content: `${text}。仍要从这里移除吗？`,
          okText: '强制移除',
          okButtonProps: { danger: true },
          cancelText: '取消',
          onOk: () => removeDevice.mutate({ id: variables.id, force: true }),
        })
        return
      }
      message.error(text)
    },
  })

  const retryDevice = useMutation({
    mutationFn: (id: string) => api.retryDevice(id),
    onSuccess: () => {
      message.info('已重新开始准备')
      queryClient.invalidateQueries({ queryKey: ['me'] })
    },
    onError: (error) => {
      message.error(error instanceof ApiError ? error.message : '重试失败')
    },
  })

  const { disconnect, reconnect } = useDeviceConnection()

  const devices = me.data?.devices ?? []
  const apps = useQuery(appsQueryOptions)
  const appList = apps.data?.apps ?? []

  return (
    <div className="page">
      <PageHeader
        eyebrow="Agents"
        title="智能体"
        description="看看它是否准备好，随时开口和它说话。"
        actions={
          <Space>
            <Button icon={<ReloadOutlined />} loading={me.isFetching} onClick={() => me.refetch()}>
              刷新
            </Button>
            {devices.length ? (
              <Button type="primary" icon={<PlusOutlined />} onClick={() => setAddOpen(true)}>
                添加设备
              </Button>
            ) : null}
          </Space>
        }
      />

      {me.error && !(me.error instanceof ApiError && me.error.status === 401) ? (
        <Alert
          type="error"
          showIcon
          className="panel-alert"
          message="暂时无法加载你的智能体"
          description={me.error.message}
        />
      ) : null}

      {devices.length ? (
        devices.map((device) => {
          const ownedApps = appList.filter((app) => deviceForApp([device], app) !== undefined)
          return (
            <section className="surface" key={device.id}>
              <DevicePanel
                device={device}
                creating={createDevice.isPending}
                retrying={retryDevice.isPending && retryDevice.variables === device.id}
                onCreate={() => createDevice.mutate({})}
                onRemove={devices.length > 1 ? () => removeDevice.mutate({ id: device.id }) : undefined}
                removing={removeDevice.isPending && removeDevice.variables?.id === device.id}
                onRetry={() => retryDevice.mutate(device.id)}
                disconnecting={disconnect.isPending && disconnect.variables === device.id}
                reconnecting={reconnect.isPending && reconnect.variables === device.id}
                onDisconnect={() => disconnect.mutate(device.id)}
                onReconnect={() => reconnect.mutate(device.id)}
                onCall={() => navigate({ to: '/portal/call', search: { device: device.id } })}
              />
              {ownedApps.length ? (
                <div className="device-apps">
                  <div className="section-header">
                    <div>
                      <h2>它为这些场合做过的应用</h2>
                      <p>每一个都来自你的一句话。</p>
                    </div>
                    <Link to="/portal/apps" className="text-link">
                      全部应用
                    </Link>
                  </div>
                  <div className="data-list">
                    {ownedApps.map((app) => (
                      <AppRow key={app.app_id} app={app} device={device} />
                    ))}
                  </div>
                </div>
              ) : null}
            </section>
          )
        })
      ) : (
        <section className="surface">
          <DevicePanel
            creating={createDevice.isPending}
            retrying={false}
            onCreate={() => createDevice.mutate({})}
            onRetry={() => undefined}
            onCall={() => navigate({ to: '/portal/call' })}
          />
        </section>
      )}

      <AddDeviceModal
        open={addOpen}
        devices={devices}
        submitting={createDevice.isPending}
        onCancel={() => setAddOpen(false)}
        onSubmit={(input) => createDevice.mutate(input)}
      />
    </div>
  )
}
