import {
  AppstoreOutlined,
  DashboardOutlined,
  DesktopOutlined,
  HistoryOutlined,
  HomeOutlined,
  RobotOutlined,
  SettingOutlined,
  TeamOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { meQueryOptions } from '../../api/queries'
import { ConsoleLayout, type ConsoleNavGroup, type ConsoleNavItem } from './ConsoleLayout'

const portalNavItems: readonly ConsoleNavItem[] = [
  { to: '/portal', label: '概览', icon: <HomeOutlined />, exact: true },
  { to: '/portal/agents', label: '智能体', icon: <RobotOutlined />, exact: false },
  { to: '/portal/apps', label: '应用', icon: <AppstoreOutlined />, exact: false },
  { to: '/portal/screens', label: '屏幕', icon: <DesktopOutlined />, exact: false },
  { to: '/portal/services', label: '屏幕连接', icon: <ThunderboltOutlined />, exact: false },
  { to: '/portal/activity', label: '动态', icon: <HistoryOutlined />, exact: false },
  { to: '/portal/settings', label: '设置', icon: <SettingOutlined />, exact: false },
]

// Admin entries sit in the same sidebar so administrators do not have to go
// through settings to reach them. Non-admins never receive these items; the
// /admin routes still enforce the role on their own.
const adminNavItems: readonly ConsoleNavItem[] = [
  { to: '/admin', label: '管理概览', icon: <DashboardOutlined />, exact: true },
  { to: '/admin/users', label: '用户', icon: <TeamOutlined />, exact: false },
  { to: '/admin/agents', label: '全部智能体', icon: <RobotOutlined />, exact: false },
  { to: '/admin/apps', label: '全部应用', icon: <AppstoreOutlined />, exact: false },
  { to: '/admin/settings', label: '平台设置', icon: <SettingOutlined />, exact: false },
]

export function PortalLayout() {
  const me = useQuery(meQueryOptions)
  const groups: readonly ConsoleNavGroup[] =
    me.data?.user.role === 'admin'
      ? [{ items: portalNavItems }, { title: '管理', items: adminNavItems }]
      : [{ items: portalNavItems }]

  return <ConsoleLayout groups={groups} />
}
