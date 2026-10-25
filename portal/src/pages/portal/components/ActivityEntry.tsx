import {
  ApiOutlined,
  AppstoreAddOutlined,
  CheckCircleOutlined,
  DesktopOutlined,
  PlayCircleOutlined,
  PoweroffOutlined,
  RobotOutlined,
} from '@ant-design/icons'
import { Link } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import type { ActivityItem as Activity } from '../../../lib/activity'
import { formatDate, formatRelative } from '../../../lib/format'

const icons: Record<Activity['kind'], ReactNode> = {
  'device-created': <RobotOutlined />,
  'device-registered': <ApiOutlined />,
  'app-build-started': <AppstoreAddOutlined />,
  'app-built': <CheckCircleOutlined />,
  'app-started': <PlayCircleOutlined />,
  'app-stopped': <PoweroffOutlined />,
}

export function ActivityEntry({ item }: { item: Activity }) {
  const content = (
    <>
      <span className={`activity-icon activity-icon-${item.kind}`}>{icons[item.kind]}</span>
      <span className="activity-body">
        <span className="activity-title">{item.title}</span>
        {item.detail ? <span className="activity-detail">{item.detail}</span> : null}
      </span>
      <time className="activity-time" dateTime={item.at} title={formatDate(item.at)}>
        {formatRelative(item.at)}
      </time>
    </>
  )

  if (item.appId) {
    return (
      <Link
        to="/portal/apps/$appId"
        params={{ appId: item.appId }}
        className="activity-item"
      >
        {content}
      </Link>
    )
  }
  if (item.deviceId) {
    return (
      <Link to="/portal/agents" className="activity-item">
        {content}
      </Link>
    )
  }
  return <div className="activity-item">{content}</div>
}

export function ActivityGroupTitle({ label, icon }: { label: string; icon?: ReactNode }) {
  return (
    <div className="activity-group-title">
      {icon ?? <DesktopOutlined />}
      <span>{label}</span>
    </div>
  )
}
