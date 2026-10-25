import {
  AppstoreOutlined,
  AudioOutlined,
  CloudServerOutlined,
  DesktopOutlined,
  PlusOutlined,
  ReloadOutlined,
  RobotOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { Alert, App as AntApp, Button, Skeleton, Space, Tag } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { ApiError, api } from '../../api'
import { appsQueryOptions, meQueryOptions, screensQueryOptions, servicesQueryOptions } from '../../api/queries'
import { EmptyState } from '../../components/EmptyState'
import { usePageTitle } from '../../hooks/usePageTitle'
import { primaryDevice } from '../../lib/device'
import { AppRow } from './components/AppRow'
import { DevicePanel } from './components/DevicePanel'
import { PageHeader } from './components/PageHeader'
import { ScreenRow } from './components/ScreenRow'
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
  const screens = useQuery(screensQueryOptions)
  const services = useQuery(servicesQueryOptions)

  const createDevice = useMutation({
    mutationFn: api.createDevice,
    onSuccess: () => {
      message.success('正在为你创建智能体')
      queryClient.invalidateQueries({ queryKey: ['me'] })
    },
    onError: (error) => {
      message.error(error instanceof ApiError ? error.message : '创建智能体失败')
    },
  })

  const retryDevice = useMutation({
    mutationFn: (id: string) => api.retryDevice(id),
    onSuccess: () => {
      message.info('已重新开始开通流程')
      queryClient.invalidateQueries({ queryKey: ['me'] })
    },
    onError: (error) => {
      message.error(error instanceof ApiError ? error.message : '重试失败')
    },
  })

  const devices = me.data?.devices ?? []
  const appList = apps.data?.apps ?? []
  const screenList = screens.data?.screens ?? []
  const device = primaryDevice(devices)
  const registered = devices.filter((item) => item.state === 'registered').length
  const provisioning = devices.filter((item) => item.state === 'provisioning').length
  const building = appList.filter((app) => app.state === 'building').length
  const running = appList.filter((app) => app.state === 'running').length
  const onlineScreens = screenList.filter((screen) => screen.online).length
  const refreshing = me.isFetching || apps.isFetching || screens.isFetching

  const refreshAll = () => {
    void me.refetch()
    void apps.refetch()
    void screens.refetch()
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
        eyebrow="Portal"
        title={`你好，${me.data?.user.username ?? ''}`}
        description="智能体、应用、屏幕与这场活动的实时状态都在这里。"
        actions={
          <Space wrap>
            <Button icon={<ReloadOutlined />} loading={refreshing} onClick={refreshAll}>
              刷新
            </Button>
            {device?.state === 'registered' ? (
              <Button type="primary" icon={<AudioOutlined />} onClick={() => call(device.id)}>
                发起通话
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
          message="无法加载账号与智能体信息"
          description={me.error.message}
        />
      ) : null}

      {services.data && !services.data.ready ? (
        <Alert
          type="warning"
          showIcon
          className="panel-alert"
          message="屏幕服务还没有准备好"
          description="应用可以继续生成，但暂时不能启动和投屏。请先完成服务绑定，或联系管理员配置平台默认服务。"
          action={
            <Link to="/portal/services">
              <Button size="small">去绑定服务</Button>
            </Link>
          }
        />
      ) : null}

      <div className="stat-grid">
        <StatTile
          icon={<RobotOutlined />}
          label="智能体"
          value={devices.length}
          note={devices.length ? `${registered} 个已注册` : '尚未创建'}
          tone="primary"
        />
        <StatTile
          icon={<AppstoreOutlined />}
          label="应用"
          value={appList.length}
          note={appList.length ? `${running} 个正在展示 · ${building} 个构建中` : '暂无应用'}
        />
        <StatTile
          icon={<DesktopOutlined />}
          label="屏幕"
          value={screens.error ? '—' : screenList.length}
          note={
            screens.error
              ? '投屏服务暂时不可用'
              : screenList.length
                ? `${onlineScreens} 个在线`
                : '尚未发现屏幕'
          }
          tone={screens.error ? 'danger' : 'neutral'}
        />
        <StatTile
          icon={<ThunderboltOutlined />}
          label="进行中"
          value={building + provisioning}
          note={building + provisioning ? `${building} 个构建 · ${provisioning} 个开通` : '没有进行中的任务'}
          tone={building + provisioning ? 'warning' : 'neutral'}
        />
      </div>

      {services.data ? (
        <div className="service-status">
          <CloudServerOutlined aria-hidden="true" />
          <span>屏幕服务</span>
          <Tag>{services.data.mode === 'custom' ? '自有服务' : '平台托管'}</Tag>
          <Tag color={services.data.ready ? 'green' : 'orange'}>
            {services.data.ready ? '已就绪' : '未就绪'}
          </Tag>
          <Link to="/portal/services" className="text-link">
            管理服务
          </Link>
        </div>
      ) : null}

      <div className="overview-grid">
        <section className="surface">
          <div className="section-header">
            <div>
              <h2>我的智能体</h2>
              <p>通过语音创建应用、控制屏幕的那个智能体。</p>
            </div>
            <Link to="/portal/agents" className="text-link">
              智能体管理
            </Link>
          </div>
          <DevicePanel
            device={device}
            creating={createDevice.isPending}
            retrying={retryDevice.isPending}
            onCreate={() => createDevice.mutate()}
            onRetry={() => device && retryDevice.mutate(device.id)}
            onCall={() => call(device?.id)}
          />
        </section>

        <section className="surface">
          <div className="section-header">
            <div>
              <h2>屏幕</h2>
              <p>身边正在或可以被投屏的显示设备。</p>
            </div>
            <Link to="/portal/screens" className="text-link">
              全部屏幕
            </Link>
          </div>
          {screens.isPending ? (
            <Skeleton active paragraph={{ rows: 4 }} title={false} />
          ) : screens.error ? (
            <Alert
              type="warning"
              showIcon
              message="无法发现屏幕"
              description={
                screens.error instanceof ApiError ? screens.error.message : '投屏服务暂时不可用。'
              }
            />
          ) : screenList.length ? (
            <div className="data-list">
              {screenList.slice(0, 4).map((screen) => (
                <ScreenRow key={screen.screen_id} screen={screen} />
              ))}
            </div>
          ) : (
            <EmptyState
              icon={<DesktopOutlined />}
              title="还没有发现屏幕"
              description="屏幕由投屏服务自动发现。投屏动作通过语音触发，不需要在 Portal 里手动操作。"
            />
          )}
        </section>
      </div>

      <section className="surface">
        <div className="section-header">
          <div>
            <h2>最近应用</h2>
            <p>由语音生成、正在构建或已经投上屏幕的应用。</p>
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
                screenName={
                  app.screen_id
                    ? screenList.find((screen) => screen.screen_id === app.screen_id)?.name
                    : undefined
                }
              />
            ))}
          </div>
        ) : (
          <EmptyState
            icon={<PlusOutlined />}
            title={device ? '还没有应用' : '先创建你的智能体'}
            description={
              device?.state === 'registered'
                ? '点「发起通话」，直接对智能体说出你想做的事。'
                : '智能体完成开通后，就可以发起通话并创建应用了。'
            }
            action={
              device?.state === 'registered' ? (
                <Button type="primary" icon={<AudioOutlined />} onClick={() => call(device.id)}>
                  发起通话
                </Button>
              ) : !device ? (
                <Button
                  type="primary"
                  icon={<PlusOutlined />}
                  loading={createDevice.isPending}
                  onClick={() => createDevice.mutate()}
                >
                  创建我的智能体
                </Button>
              ) : null
            }
          />
        )}
      </section>
    </div>
  )
}
