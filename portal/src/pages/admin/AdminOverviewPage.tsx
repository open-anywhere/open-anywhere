import {
  AppstoreOutlined,
  CheckCircleOutlined,
  ReloadOutlined,
  RobotOutlined,
  TeamOutlined,
} from '@ant-design/icons'
import { Alert, Button, Skeleton, Space, Tag } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ApiError } from '../../api'
import { adminOverviewQueryOptions } from '../../api/queries'
import { usePageTitle } from '../../hooks/usePageTitle'
import { PageHeader } from '../portal/components/PageHeader'
import { StatTile } from '../portal/components/StatTile'

export function AdminOverviewPage() {
  usePageTitle('管理概览')
  const overview = useQuery(adminOverviewQueryOptions)

  const data = overview.data
  const missing = data
    ? [
        !data.platform.agentapi ? 'AgentAPI' : null,
        !data.platform.linx ? 'Linx' : null,
        !data.platform.services ? '默认屏幕服务' : null,
      ].filter(Boolean)
    : []

  return (
    <div className="page">
      <PageHeader
        eyebrow="Admin"
        title="平台概览"
        description="用户、智能体、应用和上游配置的当前状态。"
        actions={
          <Space wrap>
            <Button
              icon={<ReloadOutlined />}
              loading={overview.isFetching}
              onClick={() => overview.refetch()}
            >
              刷新
            </Button>
            <Link to="/admin/settings">
              <Button type="primary">平台设置</Button>
            </Link>
          </Space>
        }
      />

      {overview.error ? (
        <Alert
          type="error"
          showIcon
          className="panel-alert"
          message="无法加载平台概览"
          description={
            overview.error instanceof ApiError ? overview.error.message : String(overview.error)
          }
        />
      ) : null}

      {missing.length ? (
        <Alert
          type="warning"
          showIcon
          className="panel-alert"
          message={`还有上游服务未配置：${missing.join('、')}`}
          description="未配置时，新用户注册后无法完成智能体开通，已生成的应用也无法启动。"
          action={
            <Link to="/admin/settings">
              <Button size="small">去配置</Button>
            </Link>
          }
        />
      ) : null}

      {overview.isPending ? (
        <section className="surface">
          <Skeleton active paragraph={{ rows: 6 }} />
        </section>
      ) : data ? (
        <>
          <div className="stat-grid">
            <StatTile
              icon={<TeamOutlined />}
              label="用户"
              value={data.users.total}
              note={`${data.users.active} 个启用 · ${data.users.disabled} 个停用`}
              tone="primary"
            />
            <StatTile
              icon={<RobotOutlined />}
              label="智能体"
              value={data.agents.total}
              note={`${data.agents.registered} 个已注册 · ${data.agents.provisioning} 个开通中`}
              tone={data.agents.error ? 'warning' : 'neutral'}
            />
            <StatTile
              icon={<AppstoreOutlined />}
              label="应用"
              value={data.apps.total}
              note={`${data.apps.running} 个运行中 · ${data.apps.building} 个构建中`}
              tone={data.apps.error ? 'danger' : 'neutral'}
            />
            <StatTile
              icon={<CheckCircleOutlined />}
              label="注册入口"
              value={data.platform.registration_open ? '开放' : '关闭'}
              note={data.platform.registration_open ? '任何人都可以注册' : '仅管理员可创建账号'}
              tone={data.platform.registration_open ? 'success' : 'neutral'}
            />
          </div>

          <section className="surface">
            <div className="section-header">
              <div>
                <h2>上游配置</h2>
                <p>平台密钥和地址由管理员维护，新构建与新开通会立即使用最新值。</p>
              </div>
              <Link to="/admin/settings" className="text-link">
                平台设置
              </Link>
            </div>
            <Space wrap size="middle">
              <span>
                AgentAPI：{' '}
                {data.platform.agentapi ? <Tag color="green">已配置</Tag> : <Tag color="red">未配置</Tag>}
              </span>
              <span>
                Linx：{' '}
                {data.platform.linx ? <Tag color="green">已配置</Tag> : <Tag color="red">未配置</Tag>}
              </span>
              <span>
                默认屏幕服务：{' '}
                {data.platform.services ? <Tag color="green">已配置</Tag> : <Tag color="orange">未配置</Tag>}
              </span>
              <span>
                管理员：<strong>{data.users.admins}</strong> 个
              </span>
              <span>
                开通失败：<strong>{data.agents.error}</strong> 个
              </span>
            </Space>
          </section>
        </>
      ) : null}
    </div>
  )
}
