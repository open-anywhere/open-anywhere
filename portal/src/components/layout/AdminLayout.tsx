import {
  AppstoreOutlined,
  DashboardOutlined,
  RobotOutlined,
  SettingOutlined,
  TeamOutlined,
} from '@ant-design/icons'
import { ConsoleLayout, type ConsoleNavItem } from './ConsoleLayout'

const navItems: readonly ConsoleNavItem[] = [
  { to: '/admin', label: '概览', icon: <DashboardOutlined />, exact: true },
  { to: '/admin/users', label: '用户', icon: <TeamOutlined />, exact: false },
  { to: '/admin/agents', label: '智能体', icon: <RobotOutlined />, exact: false },
  { to: '/admin/apps', label: '应用', icon: <AppstoreOutlined />, exact: false },
  { to: '/admin/settings', label: '平台设置', icon: <SettingOutlined />, exact: false },
]

export function AdminLayout() {
  return <ConsoleLayout mode="admin" navItems={navItems} />
}
