import { MobileOutlined, ReloadOutlined } from '@ant-design/icons'
import { Alert, App as AntApp, Button, Space, Tag } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { ApiError, api, type CreateDeviceInput, type Device } from '../../api'
import { appsQueryOptions, meQueryOptions } from '../../api/queries'
import { useDeviceConnection } from '../../hooks/useDeviceConnection'
import { usePageTitle } from '../../hooks/usePageTitle'
import { deviceForApp } from '../../lib/device'
import { AgentSettingsModal } from './components/AgentSettingsModal'
import { AppRow } from './components/AppRow'
import { DevicePanel } from './components/DevicePanel'
import { PageHeader } from './components/PageHeader'

export function AgentsPage() {
  usePageTitle('智能体')
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { message } = AntApp.useApp()

  const me = useQuery({
    ...meQueryOptions,
    refetchInterval: (query) =>
      query.state.data?.devices.some((device) => device.state === 'provisioning') ? 2_000 : false,
  })

  const [settingsDevice, setSettingsDevice] = useState<string>()

  const createDevice = useMutation({
    mutationFn: (input?: CreateDeviceInput) => api.createDevice(input),
    onSuccess: () => {
      message.success('正在准备你的智能体')
      queryClient.invalidateQueries({ queryKey: ['me'] })
      queryClient.invalidateQueries({ queryKey: ['apps'] })
    },
    onError: (error) => {
      message.error(error instanceof ApiError ? error.message : '暂时无法添加设备')
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
  // Hardware joins an agent a virtual device created, so the account's agents
  // are the distinct agent IDs. A device that has no agent yet stands alone.
  const groups = new Map<string, Device[]>()
  for (const device of devices) {
    const key = device.agent_id || device.id
    groups.set(key, [...(groups.get(key) ?? []), device])
  }
  const apps = useQuery(appsQueryOptions)
  const appList = apps.data?.apps ?? []

  return (
    <div className="page">
      <PageHeader
        eyebrow="Agents"
        title="智能体"
        description="看看它是否准备好，随时开口和它说话。设备在“设备”页管理。"
        actions={
          <Space>
            <Button icon={<ReloadOutlined />} loading={me.isFetching} onClick={() => me.refetch()}>
              刷新
            </Button>
            <Link to="/portal/devices">
              <Button icon={<MobileOutlined />}>管理设备</Button>
            </Link>
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
        [...groups.values()].map((members) => {
          // The browser device speaks for the agent; hardware only shares it.
          const device = members.find((member) => member.kind === 'virtual') ?? members[0]
          const ownedApps = appList.filter((app) => deviceForApp(members, app) !== undefined)
          return (
            <section className="surface" key={device.id}>
              <DevicePanel
                device={device}
                creating={createDevice.isPending}
                retrying={retryDevice.isPending && retryDevice.variables === device.id}
                onCreate={() => createDevice.mutate({})}
                onSettings={() => setSettingsDevice(device.id)}
                onRetry={() => retryDevice.mutate(device.id)}
                disconnecting={disconnect.isPending && disconnect.variables === device.id}
                reconnecting={reconnect.isPending && reconnect.variables === device.id}
                onDisconnect={() => disconnect.mutate(device.id)}
                onReconnect={() => reconnect.mutate(device.id)}
                onCall={() => navigate({ to: '/portal/call', search: { device: device.id } })}
              />
              <p className="device-note">
                <MobileOutlined /> 这个智能体下有 {members.length} 台设备：
                {members.map((member) => (
                  <Tag key={member.id} style={{ marginLeft: 6 }}>
                    {member.name || (member.kind === 'physical' ? '硬件设备' : '虚拟设备')}
                  </Tag>
                ))}
                <Link to="/portal/devices" className="text-link" style={{ marginLeft: 12 }}>
                  管理设备
                </Link>
              </p>
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

      <AgentSettingsModal
        deviceId={settingsDevice}
        reconnectable={devices.find((device) => device.id === settingsDevice)?.kind === 'virtual'}
        onClose={() => setSettingsDevice(undefined)}
      />
    </div>
  )
}
