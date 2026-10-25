import { AudioOutlined, ReloadOutlined, SearchOutlined } from '@ant-design/icons'
import { Alert, Button, Input, Select, Skeleton, Space } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useMemo, useState } from 'react'
import type { AppState } from '../../api'
import { ApiError } from '../../api'
import { appsQueryOptions, meQueryOptions, screensQueryOptions } from '../../api/queries'
import { EmptyState } from '../../components/EmptyState'
import { usePageTitle } from '../../hooks/usePageTitle'
import { deviceForApp, primaryDevice } from '../../lib/device'
import { appStateLabels } from '../../lib/format'
import { AppRow } from './components/AppRow'
import { PageHeader } from './components/PageHeader'

const stateOptions: { value: AppState | 'all'; label: string }[] = [
  { value: 'all', label: '全部状态' },
  { value: 'building', label: appStateLabels.building },
  { value: 'running', label: appStateLabels.running },
  { value: 'idle', label: appStateLabels.idle },
  { value: 'error', label: appStateLabels.error },
]

export function AppsPage() {
  usePageTitle('应用')
  const navigate = useNavigate()
  const [search, setSearch] = useState('')
  const [state, setState] = useState<AppState | 'all'>('all')
  const [deviceKey, setDeviceKey] = useState<string>('all')

  const me = useQuery(meQueryOptions)
  const screens = useQuery(screensQueryOptions)
  const apps = useQuery({
    ...appsQueryOptions,
    refetchOnWindowFocus: true,
    refetchInterval: (query) =>
      query.state.data?.apps.some((app) => app.state === 'building') ? 5_000 : false,
  })

  const devices = me.data?.devices ?? []
  const device = primaryDevice(devices)
  const appList = apps.data?.apps ?? []
  const screenList = screens.data?.screens ?? []

  const selectedDevice = devices.find((item) => item.mac === deviceKey)
  const visible = useMemo(() => {
    const needle = search.trim().toLowerCase()
    return appList.filter((app) => {
      if (state !== 'all' && app.state !== state) return false
      if (
        selectedDevice &&
        app.device_id !== selectedDevice.mac &&
        app.device_id !== selectedDevice.id &&
        app.device_id !== selectedDevice.agent_id
      ) {
        return false
      }
      if (!needle) return true
      return [app.title, app.name, app.summary, app.app_id]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(needle))
    })
  }, [appList, search, state, selectedDevice])

  const filtered = search.trim() !== '' || state !== 'all' || deviceKey !== 'all'

  return (
    <div className="page">
      <PageHeader
        eyebrow="Applications"
        title="应用"
        description="通过语音生成的所有应用，以及它们此刻的状态与所在屏幕。"
        actions={
          <Space wrap>
            <Button
              icon={<ReloadOutlined />}
              loading={apps.isFetching}
              onClick={() => apps.refetch()}
            >
              刷新
            </Button>
            {device?.state === 'registered' ? (
              <Button
                type="primary"
                icon={<AudioOutlined />}
                onClick={() => navigate({ to: '/portal/call', search: { device: device.id } })}
              >
                发起通话
              </Button>
            ) : null}
          </Space>
        }
      />

      {apps.error ? (
        <Alert
          type="error"
          showIcon
          className="panel-alert"
          message="无法加载应用列表"
          description={apps.error instanceof ApiError ? apps.error.message : String(apps.error)}
        />
      ) : null}

      <div className="toolbar">
        <Input
          allowClear
          prefix={<SearchOutlined />}
          placeholder="搜索应用名称或 ID"
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          className="toolbar-search"
        />
        <Select
          value={state}
          onChange={setState}
          options={stateOptions}
          className="toolbar-select"
          aria-label="按状态筛选"
        />
        {devices.length > 1 ? (
          <Select
            value={deviceKey}
            onChange={setDeviceKey}
            className="toolbar-select"
            aria-label="按智能体筛选"
            options={[
              { value: 'all', label: '全部智能体' },
              ...devices.map((item) => ({
                value: item.mac,
                label: item.agent_name || item.mac,
              })),
            ]}
          />
        ) : null}
        <span className="toolbar-count">
          {filtered ? `${visible.length} / ${appList.length}` : appList.length} 个应用
        </span>
      </div>

      {apps.isPending ? (
        <section className="surface">
          <Skeleton active paragraph={{ rows: 6 }} title={false} />
        </section>
      ) : visible.length ? (
        <section className="surface surface-flush">
          <div className="data-list">
            {visible.map((app) => (
              <AppRow
                key={app.app_id}
                app={app}
                device={deviceForApp(devices, app)}
                screenName={
                  app.screen_id
                    ? screenList.find((screen) => screen.screen_id === app.screen_id)?.name
                    : undefined
                }
              />
            ))}
          </div>
        </section>
      ) : (
        <section className="surface">
          <EmptyState
            title={filtered ? '没有匹配的应用' : '还没有应用'}
            description={
              filtered
                ? '换一个关键词或筛选条件试试。'
                : device?.state === 'registered'
                  ? '点「发起通话」，直接对智能体说出你想做的事。'
                  : '智能体完成开通后，就可以发起通话并创建应用了。'
            }
            action={
              filtered ? (
                <Button
                  onClick={() => {
                    setSearch('')
                    setState('all')
                    setDeviceKey('all')
                  }}
                >
                  清除筛选
                </Button>
              ) : device?.state === 'registered' ? (
                <Button
                  type="primary"
                  icon={<AudioOutlined />}
                  onClick={() => navigate({ to: '/portal/call', search: { device: device.id } })}
                >
                  发起通话
                </Button>
              ) : null
            }
          />
        </section>
      )}
    </div>
  )
}
