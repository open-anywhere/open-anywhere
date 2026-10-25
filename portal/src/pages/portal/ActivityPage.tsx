import { ClockCircleOutlined, HistoryOutlined, SyncOutlined } from '@ant-design/icons'
import { Alert, Button, Skeleton } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ApiError } from '../../api'
import { appsQueryOptions, meQueryOptions } from '../../api/queries'
import { EmptyState } from '../../components/EmptyState'
import { usePageTitle } from '../../hooks/usePageTitle'
import { deriveActivity, type ActivityItem } from '../../lib/activity'
import { stepLabels } from '../../lib/format'
import { ActivityEntry } from './components/ActivityEntry'
import { PageHeader } from './components/PageHeader'

function dayKey(value: string): string {
  const date = new Date(value)
  return `${date.getFullYear()}-${date.getMonth() + 1}-${date.getDate()}`
}

function dayLabel(value: string): string {
  const date = new Date(value)
  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
  const target = new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime()
  const diff = Math.round((today - target) / 86_400_000)
  if (diff === 0) return '今天'
  if (diff === 1) return '昨天'
  return new Intl.DateTimeFormat('zh-CN', {
    year: date.getFullYear() === now.getFullYear() ? undefined : 'numeric',
    month: 'long',
    day: 'numeric',
  }).format(date)
}

function groupByDay(items: ActivityItem[]): { key: string; label: string; items: ActivityItem[] }[] {
  const groups: { key: string; label: string; items: ActivityItem[] }[] = []
  for (const item of items) {
    const key = dayKey(item.at)
    let group = groups.find((entry) => entry.key === key)
    if (!group) {
      group = { key, label: dayLabel(item.at), items: [] }
      groups.push(group)
    }
    group.items.push(item)
  }
  return groups
}

export function ActivityPage() {
  usePageTitle('活动')
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

  const devices = me.data?.devices ?? []
  const appList = apps.data?.apps ?? []
  const items = deriveActivity(devices, appList)
  const groups = groupByDay(items)
  const building = appList.filter((app) => app.state === 'building')
  const provisioning = devices.filter((device) => device.state === 'provisioning')
  const loading = me.isPending || apps.isPending
  const error = me.error ?? apps.error

  return (
    <div className="page">
      <PageHeader
        eyebrow="Activity"
        title="活动"
        description="智能体开通与应用构建、投屏的时间线。"
        actions={
          <Button
            icon={<ClockCircleOutlined />}
            loading={me.isFetching || apps.isFetching}
            onClick={() => {
              void me.refetch()
              void apps.refetch()
            }}
          >
            刷新
          </Button>
        }
      />

      <div className="hint-note">
        活动由智能体与应用的当前时间戳汇总，不含逐条操作日志；应用历史还会受 Gateway 的历史条数限制。
      </div>

      {error ? (
        <Alert
          type="error"
          showIcon
          className="panel-alert"
          message="无法加载活动"
          description={error instanceof ApiError ? error.message : String(error)}
        />
      ) : null}

      {building.length || provisioning.length ? (
        <section className="surface">
          <div className="section-header">
            <div>
              <h2>进行中</h2>
              <p>这些任务完成后会自动出现在下面的时间线里。</p>
            </div>
          </div>
          <div className="ongoing-list">
            {provisioning.map((device) => (
              <Link to="/portal/agents" className="ongoing-item" key={device.id}>
                <SyncOutlined spin className="ongoing-icon" />
                <span>
                  <strong>正在开通 {device.agent_name || device.mac}</strong>
                  <small>{stepLabels[device.step] ?? device.step}</small>
                </span>
              </Link>
            ))}
            {building.map((app) => (
              <Link
                to="/portal/apps/$appId"
                params={{ appId: app.app_id }}
                className="ongoing-item"
                key={app.app_id}
              >
                <SyncOutlined spin className="ongoing-icon" />
                <span>
                  <strong>正在构建「{app.title || app.name || '未命名应用'}」</strong>
                  <small>{app.progress || '等待构建结果'}</small>
                </span>
              </Link>
            ))}
          </div>
        </section>
      ) : null}

      {loading ? (
        <section className="surface">
          <Skeleton active paragraph={{ rows: 6 }} title={false} />
        </section>
      ) : groups.length ? (
        <section className="surface">
          <div className="activity-groups">
            {groups.map((group) => (
              <div className="activity-group" key={group.key}>
                <div className="activity-group-title">{group.label}</div>
                <div className="activity-list">
                  {group.items.map((item) => (
                    <ActivityEntry key={item.id} item={item} />
                  ))}
                </div>
              </div>
            ))}
          </div>
        </section>
      ) : !error ? (
        <section className="surface">
          <EmptyState
            icon={<HistoryOutlined />}
            title="还没有活动"
            description="创建智能体、通过语音生成应用之后，这里会记录关键的时间点。"
          />
        </section>
      ) : null}
    </div>
  )
}
