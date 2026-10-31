import {
  AppstoreOutlined,
  AudioOutlined,
  CloudServerOutlined,
  PlusOutlined,
  ReloadOutlined,
  RobotOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { Alert, App as AntApp, Button, Skeleton, Space, Tag } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { ApiError, api } from '../../api'
import { appsQueryOptions, meQueryOptions, servicesQueryOptions } from '../../api/queries'
import { EmptyState } from '../../components/EmptyState'
import { useDeviceConnection } from '../../hooks/useDeviceConnection'
import { usePageTitle } from '../../hooks/usePageTitle'
import { primaryDevice } from '../../lib/device'
import { AppRow } from './components/AppRow'
import { DevicePanel } from './components/DevicePanel'
import { PageHeader } from './components/PageHeader'
import { StatTile } from './components/StatTile'

export function OverviewPage() {
  usePageTitle('概览')
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { message } = AntApp.useApp()

  const me = useQuery({
    ...meQueryOptions,
    refetchInterval: (query) =>
      query.state.data?.devices.some((device) => device.state === 'provisioning') ? 2_000 : false,
  })
  const apps = useQuery({
    ...appsQueryOptions,
    refetchOnWindowFocus: true,
    refetchInterval: (query) =>
      query.state.data?.apps.some((app) => app.state === 'building') ? 5_000 : false,
  })
  const services = useQuery(servicesQueryOptions)

  const createDevice = useMutation({
    mutationFn: api.createDevice,
    onSuccess: () => {
      message.success('正在准备你的智能体')
      queryClient.invalidateQueries({ queryKey: ['me'] })
    },
    onError: (error) => {
      message.error(error instanceof ApiError ? error.message : '暂时无法创建智能体')
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
  const appList = apps.data?.apps ?? []
  const device = primaryDevice(devices)
  const registered = devices.filter((item) => item.state === 'registered').length
  const provisioning = devices.filter((item) => item.state === 'provisioning').length
  const building = appList.filter((app) => app.state === 'building').length
  const running = appList.filter((app) => app.state === 'running').length
  const refreshing = me.isFetching || apps.isFetching

  const refreshAll = () => {
    void me.refetch()
    void apps.refetch()
  }

  const call = (deviceId?: string) => {
    if (deviceId) {
      navigate({ to: '/portal/call', search: { device: deviceId } })
    } else {
      navigate({ to: '/portal/call' })
    }
  }

  return (
    <div className="page">
      <PageHeader
        eyebrow="我的空间"
        title={`你好，${me.data?.user.username ?? ''}`}
        description="你的智能体、做过的应用，和身边的屏幕，都在这里。"
        actions={
          <Space wrap>
            <Button icon={<ReloadOutlined />} loading={refreshing} onClick={refreshAll}>
              刷新
            </Button>
            {device?.state === 'registered' ? (
              <Button type="primary" icon={<AudioOutlined />} onClick={() => call(device.id)}>
                开始对话
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
          message="暂时无法加载你的空间"
          description={me.error.message}
        />
      ) : null}

      {services.data && !services.data.ready ? (
        <Alert
          type="warning"
          showIcon
          className="panel-alert"
          message="应用运行服务还没有准备好"
          description="应用可以继续生成，但暂时不能出现在屏幕上。可以先检查屏幕连接，或稍后再试。"
          action={
            <Link to="/portal/services">
              <Button size="small">检查连接</Button>
            </Link>
          }
        />
      ) : null}

      <div className="stat-grid">
        <StatTile
          icon={<RobotOutlined />}
          label="智能体"
          value={devices.length}
          note={devices.length ? `${registered} 个已准备好` : '尚未创建'}
          tone="primary"
        />
        <StatTile
          icon={<AppstoreOutlined />}
          label="应用"
          value={appList.length}
          note={appList.length ? `${running} 个正在播放 · ${building} 个生成中` : '暂无应用'}
        />
        <StatTile
          icon={<ThunderboltOutlined />}
          label="进行中"
          value={building + provisioning}
          note={building + provisioning ? `${building} 个生成 · ${provisioning} 个准备` : '没有进行中的任务'}
          tone={building + provisioning ? 'warning' : 'neutral'}
        />
      </div>

      {services.data ? (
        <div className="service-status">
          <CloudServerOutlined aria-hidden="true" />
          <span>应用运行服务</span>
          <Tag>{services.data.mode === 'custom' ? '自己接入' : '平台提供'}</Tag>
          <Tag color={services.data.ready ? 'green' : 'orange'}>
            {services.data.ready ? '已就绪' : '未就绪'}
          </Tag>
          <Link to="/portal/services" className="text-link">
            管理连接
          </Link>
        </div>
      ) : null}

      <div className="overview-grid">
        <section className="surface">
          <div className="section-header">
            <div>
              <h2>我的智能体</h2>
              <p>听你说话、为你准备应用、控制屏幕。</p>
            </div>
            <Link to="/portal/agents" className="text-link">
              管理智能体
            </Link>
          </div>
          <DevicePanel
            device={device}
            creating={createDevice.isPending}
            retrying={retryDevice.isPending}
            onCreate={() => createDevice.mutate()}
            onRetry={() => device && retryDevice.mutate(device.id)}
            disconnecting={disconnect.isPending}
            reconnecting={reconnect.isPending}
            onDisconnect={() => device && disconnect.mutate(device.id)}
            onReconnect={() => device && reconnect.mutate(device.id)}
            onCall={() => call(device?.id)}
          />
        </section>

      </div>

      <section className="surface">
        <div className="section-header">
          <div>
            <h2>最近的应用</h2>
            <p>为不同场合生成的应用，以及它们此刻的状态。</p>
          </div>
          <Link to="/portal/apps" className="text-link">
            全部应用
          </Link>
        </div>
        {apps.isPending && device ? (
          <Skeleton active paragraph={{ rows: 4 }} title={false} />
        ) : apps.error ? (
          <Alert
            type="error"
            showIcon
            message="无法加载应用"
            description={apps.error instanceof ApiError ? apps.error.message : String(apps.error)}
          />
        ) : appList.length ? (
          <div className="data-list">
            {appList.slice(0, 5).map((app) => (
              <AppRow
                key={app.app_id}
                app={app}
                device={devices.find(
                  (item) =>
                    item.mac === app.device_id ||
                    item.id === app.device_id ||
                    item.agent_id === app.device_id,
                )}
              />
            ))}
          </div>
        ) : (
          <EmptyState
            icon={<PlusOutlined />}
            title={device ? '还没有应用' : '先准备好你的智能体'}
            description={
              device?.state === 'registered'
                ? '点「开始对话」，直接对它说出你想做的事。'
                : '智能体准备好之后，就可以开口说出第一个想法。'
            }
            action={
              device?.state === 'registered' ? (
                <Button type="primary" icon={<AudioOutlined />} onClick={() => call(device.id)}>
                  开始对话
                </Button>
              ) : !device ? (
                <Button
                  type="primary"
                  icon={<PlusOutlined />}
                  loading={createDevice.isPending}
                  onClick={() => createDevice.mutate()}
                >
                  准备好我的智能体
                </Button>
              ) : null
            }
          />
        )}
      </section>
    </div>
  )
}
