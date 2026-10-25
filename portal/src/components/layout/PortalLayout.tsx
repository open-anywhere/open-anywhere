import {
  AppstoreOutlined,
  DesktopOutlined,
  HistoryOutlined,
  HomeOutlined,
  RobotOutlined,
  SettingOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { ConsoleLayout, type ConsoleNavItem } from './ConsoleLayout'

const navItems: readonly ConsoleNavItem[] = [
  { to: '/portal', label: '概览', icon: <HomeOutlined />, exact: true },
  { to: '/portal/agents', label: '智能体', icon: <RobotOutlined />, exact: false },
  { to: '/portal/apps', label: '应用', icon: <AppstoreOutlined />, exact: false },
  { to: '/portal/screens', label: '屏幕', icon: <DesktopOutlined />, exact: false },
  { to: '/portal/services', label: '服务', icon: <ThunderboltOutlined />, exact: false },
  { to: '/portal/activity', label: '活动', icon: <HistoryOutlined />, exact: false },
  { to: '/portal/settings', label: '设置', icon: <SettingOutlined />, exact: false },
]

export function PortalLayout() {
  return <ConsoleLayout mode="portal" navItems={navItems} />
}
