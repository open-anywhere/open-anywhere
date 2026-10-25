import {
  DesktopOutlined,
  MoonOutlined,
  SunOutlined,
} from '@ant-design/icons'
import { Button, Dropdown } from 'antd'
import { useThemeMode } from './ThemeProvider'
import type { ThemeMode } from '../lib/theme'

const labels: Record<ThemeMode, string> = {
  system: '跟随系统',
  light: '浅色',
  dark: '深色',
}

export function ThemeToggle({ compact = true }: { compact?: boolean }) {
  const { mode, resolved, setMode } = useThemeMode()
  const icon =
    mode === 'system' ? (
      <DesktopOutlined />
    ) : resolved === 'dark' ? (
      <MoonOutlined />
    ) : (
      <SunOutlined />
    )

  return (
    <Dropdown
      trigger={['click']}
      menu={{
        selectable: true,
        selectedKeys: [mode],
        items: [
          { key: 'system', icon: <DesktopOutlined />, label: labels.system },
          { key: 'light', icon: <SunOutlined />, label: labels.light },
          { key: 'dark', icon: <MoonOutlined />, label: labels.dark },
        ],
        onClick: ({ key }) => setMode(key as ThemeMode),
      }}
    >
      <Button
        type="text"
        icon={icon}
        aria-label={`主题：${labels[mode]}`}
        title={`主题：${labels[mode]}`}
      >
        {compact ? null : labels[mode]}
      </Button>
    </Dropdown>
  )
}
