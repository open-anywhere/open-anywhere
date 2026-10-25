import { LogoutOutlined, MenuOutlined, UserOutlined } from '@ant-design/icons'
import { App as AntApp, Avatar, Button, Drawer, Dropdown } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, Outlet, useNavigate, useRouterState } from '@tanstack/react-router'
import { useEffect, useState, type ReactNode } from 'react'
import { ApiError, api } from '../../api'
import { meQueryOptions } from '../../api/queries'
import { Brand } from '../Brand'
import { ThemeToggle } from '../ThemeToggle'

export type ConsolePath =
  | '/portal'
  | '/portal/agents'
  | '/portal/devices'
  | '/portal/apps'
  | '/portal/screens'
  | '/portal/services'
  | '/portal/activity'
  | '/portal/settings'
  | '/admin'
  | '/admin/users'
  | '/admin/agents'
  | '/admin/apps'
  | '/admin/settings'

export interface ConsoleNavItem {
  to: ConsolePath
  label: string
  icon: ReactNode
  exact?: boolean
}

// ConsoleNavGroup renders one labelled block of navigation links. The title is
// optional so the portal links stay a plain list.
export interface ConsoleNavGroup {
  title?: string
  items: readonly ConsoleNavItem[]
}

interface ConsoleLayoutProps {
  groups: readonly ConsoleNavGroup[]
}

function NavList({
  groups,
  onNavigate,
}: {
  groups: readonly ConsoleNavGroup[]
  onNavigate?: () => void
}) {
  return (
    <nav className="portal-nav" aria-label="控制台导航">
      {groups.map((group, index) => (
        <div className="portal-nav-group" key={group.title ?? `group-${index}`}>
          {group.title ? <div className="portal-nav-title">{group.title}</div> : null}
          {group.items.map((item) => (
            <Link
              key={item.to}
              to={item.to}
              className="portal-nav-link"
              activeOptions={{ exact: item.exact }}
              activeProps={{ className: 'portal-nav-link active', 'aria-current': 'page' }}
              onClick={onNavigate}
            >
              <span className="portal-nav-icon" aria-hidden="true">
                {item.icon}
              </span>
              <span>{item.label}</span>
            </Link>
          ))}
        </div>
      ))}
    </nav>
  )
}

interface SidebarProps {
  groups: readonly ConsoleNavGroup[]
  username: string
  role: string
  adminArea: boolean
  loggingOut: boolean
  onLogout: () => void
  onNavigate?: () => void
}

function Sidebar({
  groups,
  username,
  role,
  adminArea,
  loggingOut,
  onLogout,
  onNavigate,
}: SidebarProps) {
  return (
    <div className="portal-sidebar-inner">
      <div className="portal-brand">
        <Brand to={adminArea ? '/admin' : '/portal'} />
      </div>
      <NavList groups={groups} onNavigate={onNavigate} />
      <div className="portal-sidebar-foot">
        <div className="portal-account">
          <Avatar size={28} icon={<UserOutlined />} />
          <div className="portal-account-text">
            <strong>{username || '账号'}</strong>
            <span>{role === 'admin' ? '管理员' : '成员'}</span>
          </div>
          <Dropdown
            trigger={['click']}
            menu={{
              items: [
                {
                  key: 'logout',
                  icon: <LogoutOutlined />,
                  label: '退出登录',
                  onClick: onLogout,
                },
              ],
            }}
          >
            <Button
              type="text"
              size="small"
              icon={<LogoutOutlined />}
              loading={loggingOut}
              aria-label="账号菜单"
            />
          </Dropdown>
        </div>
        <div className="portal-sidebar-actions">
          <ThemeToggle />
          <span className="portal-version">Open Anywhere</span>
        </div>
      </div>
    </div>
  )
}

// ConsoleLayout is the shared shell of the user portal and the admin console.
// Admin entries live in the same navigation and are added by the layout that
// knows the account role; the route guard still enforces access.
export function ConsoleLayout({ groups }: ConsoleLayoutProps) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const { message } = AntApp.useApp()
  const [drawerOpen, setDrawerOpen] = useState(false)
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  const me = useQuery(meQueryOptions)

  const unauthorized = me.error instanceof ApiError && me.error.status === 401
  useEffect(() => {
    if (unauthorized) {
      queryClient.clear()
      navigate({
        to: '/login',
        search: { redirect: pathname },
        replace: true,
      })
    }
  }, [unauthorized, navigate, pathname, queryClient])

  const logout = useMutation({
    mutationFn: api.logout,
    onSettled: () => {
      queryClient.clear()
      message.success('已退出登录')
      navigate({ to: '/', replace: true })
    },
  })

  const items = groups.flatMap((group) => group.items)
  const current = items.find((item) =>
    item.exact ? pathname === item.to : pathname === item.to || pathname.startsWith(`${item.to}/`),
  )
  const username = me.data?.user.username ?? ''
  const role = me.data?.user.role ?? 'user'
  const adminArea = pathname === '/admin' || pathname.startsWith('/admin/')

  return (
    <div className="portal-shell">
      <aside className="portal-sidebar">
        <Sidebar
          groups={groups}
          username={username}
          role={role}
          adminArea={adminArea}
          loggingOut={logout.isPending}
          onLogout={() => logout.mutate()}
        />
      </aside>

      <Drawer
        placement="left"
        size={260}
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
        closable={false}
        rootClassName="portal-drawer"
        styles={{ body: { padding: 0 } }}
      >
        <Sidebar
          groups={groups}
          username={username}
          role={role}
          adminArea={adminArea}
          loggingOut={logout.isPending}
          onLogout={() => {
            setDrawerOpen(false)
            logout.mutate()
          }}
          onNavigate={() => setDrawerOpen(false)}
        />
      </Drawer>

      <div className="portal-main">
        <header className="portal-topbar">
          <Button
            className="portal-menu-button"
            type="text"
            icon={<MenuOutlined />}
            onClick={() => setDrawerOpen(true)}
            aria-label="打开导航"
          />
          <div className="portal-topbar-title">
            {current?.label ?? (adminArea ? '管理后台' : '我的空间')}
          </div>
        </header>
        <main className="portal-content">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
