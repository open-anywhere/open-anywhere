import { ArrowRightOutlined } from '@ant-design/icons'
import { Link } from '@tanstack/react-router'
import type { AppSummary, Device } from '../../../api'
import { AppStateTag } from '../../../components/Tags'
import { formatDate } from '../../../lib/format'
import { deviceLabel } from '../../../lib/device'

export function AppRow({
  app,
  device,
  screenName,
}: {
  app: AppSummary
  device?: Device
  screenName?: string
}) {
  const title = app.title || app.name || '未命名应用'
  const description =
    app.state === 'building'
      ? app.progress || '正在构建，完成后会自动投到屏幕上'
      : app.state === 'error'
        ? app.error?.message || app.error?.last_agent_message || '应用遇到问题，查看详情以排查'
        : app.summary || '这个应用还没有描述'

  return (
    <Link
      to="/portal/apps/$appId"
      params={{ appId: app.app_id }}
      className="data-row app-row"
    >
      <div className="data-row-main">
        <div className="data-row-heading">
          <strong>{title}</strong>
          <AppStateTag state={app.state} />
        </div>
        <p className="data-row-description">{description}</p>
        <div className="data-row-meta">
          <span>{device ? deviceLabel(device) : app.device_id || '未绑定智能体'}</span>
          <span>{app.operation_count ? `${app.operation_count} 个操作` : '暂无操作'}</span>
          <span>{app.built_at ? `构建于 ${formatDate(app.built_at)}` : '构建中'}</span>
          {app.screen_id ? <span>{screenName || '已在屏幕上'}</span> : null}
        </div>
      </div>
      <span className="data-row-arrow" aria-hidden="true">
        <ArrowRightOutlined />
      </span>
    </Link>
  )
}
