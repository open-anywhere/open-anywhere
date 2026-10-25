import {
  ControlOutlined,
  DesktopOutlined,
  LogoutOutlined,
  MoonOutlined,
  SunOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { App as AntApp, Button, Descriptions, Segmented, Skeleton, Tag } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { api } from '../../api'
import { meQueryOptions, servicesQueryOptions } from '../../api/queries'
import { useThemeMode } from '../../components/ThemeProvider'
import { usePageTitle } from '../../hooks/usePageTitle'
import type { ThemeMode } from '../../lib/theme'
import { formatDate } from '../../lib/format'
import { PageHeader } from './components/PageHeader'

const themeOptions = [
  { value: 'system', label: '跟随系统', icon: <DesktopOutlined /> },
  { value: 'light', label: '浅色', icon: <SunOutlined /> },
  { value: 'dark', label: '深色', icon: <MoonOutlined /> },
]

export function SettingsPage() {
  usePageTitle('设置')
  const me = useQuery(meQueryOptions)
  const services = useQuery(servicesQueryOptions)
  const { mode, setMode } = useThemeMode()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const { message } = AntApp.useApp()

  const logout = useMutation({
    mutationFn: api.logout,
    onSettled: () => {
      queryClient.clear()
      message.success('已退出登录')
      navigate({ to: '/', replace: true })
    },
  })

  const user = me.data?.user

  return (
    <div className="page">
      <PageHeader
        eyebrow="Settings"
        title="设置"
        description="账号、服务与外观。"
      />

      <section className="surface settings-section">
        <div className="section-header">
          <div>
            <h2>账号</h2>
            <p>普通用户自助注册；管理员可以停用账号并回收资源。</p>
          </div>
          {user?.role === 'admin' ? (
            <Link to="/admin">
              <Button icon={<ControlOutlined />}>进入管理后台</Button>
            </Link>
          ) : null}
        </div>
        {me.isPending ? (
          <Skeleton active paragraph={{ rows: 3 }} title={false} />
        ) : user ? (
          <Descriptions
            size="small"
            column={{ xs: 1, sm: 2 }}
            items={[
              { key: 'username', label: '用户名', children: user.username },
              {
                key: 'role',
                label: '账号角色',
                children: (
                  <Tag color={user.role === 'admin' ? 'gold' : 'default'}>
                    {user.role === 'admin' ? '管理员' : '普通用户'}
                  </Tag>
                ),
              },
              { key: 'created', label: '注册时间', children: formatDate(user.created_at) },
              { key: 'login', label: '最后登录', children: formatDate(user.last_login_at) },
            ]}
          />
        ) : (
          <p className="muted-text">无法读取账号信息。</p>
        )}
      </section>

      <section className="surface settings-section">
        <div className="section-header">
          <div>
            <h2>屏幕服务</h2>
            <p>语音生成的应用会使用你当前绑定的 Render 与投屏服务。</p>
          </div>
          <Link to="/portal/services">
            <Button icon={<ThunderboltOutlined />}>管理服务</Button>
          </Link>
        </div>
        {services.data ? (
          <Descriptions
            size="small"
            column={{ xs: 1, sm: 2 }}
            items={[
              {
                key: 'mode',
                label: '服务模式',
                children: services.data.mode === 'managed' ? '平台托管' : '自有服务',
              },
              {
                key: 'ready',
                label: '状态',
                children: services.data.ready ? (
                  <Tag color="green">已就绪</Tag>
                ) : (
                  <Tag color="orange">不可用</Tag>
                ),
              },
            ]}
          />
        ) : (
          <p className="muted-text">无法读取服务绑定。</p>
        )}
      </section>

      <section className="surface settings-section">
        <div className="section-header">
          <div>
            <h2>外观</h2>
            <p>主题会立即应用，并保存在这台设备的浏览器里。</p>
          </div>
        </div>
        <Segmented
          value={mode}
          onChange={(value) => setMode(value as ThemeMode)}
          options={themeOptions.map((option) => ({
            value: option.value,
            label: (
              <span className="theme-option">
                {option.icon}
                {option.label}
              </span>
            ),
          }))}
        />
      </section>

      <section className="surface settings-section">
        <div className="section-header">
          <div>
            <h2>会话</h2>
            <p>退出后需要重新登录。禁用账号会让所有会话立即失效。</p>
          </div>
        </div>
        <Button
          danger
          icon={<LogoutOutlined />}
          loading={logout.isPending}
          onClick={() => logout.mutate()}
        >
          退出登录
        </Button>
      </section>
    </div>
  )
}
