import {
  AppstoreOutlined,
  CheckCircleOutlined,
  ReloadOutlined,
  RobotOutlined,
  TeamOutlined,
} from '@ant-design/icons'
import { Alert, Button, Skeleton, Space, Steps, Tag } from 'antd'
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
        !data.platform.mcp ? '灵矽 MCP 地址' : null,
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

          {missing.length ? (
            <section className="surface">
              <div className="section-header">
                <div>
                  <h2>配置向导</h2>
                  <p>按顺序完成下面几步，用户才能注册后直接使用。每一项都在平台设置里填写，页面上写明了数据从哪里取。</p>
                </div>
                <Link to="/admin/settings">
                  <Button type="primary">去配置</Button>
                </Link>
              </div>
              <Alert
                type="warning"
                showIcon
                className="panel-alert"
                message="请备份主密钥"
                description="平台密钥和 MCP Token 用主密钥加密保存。请把 portal-data/secret.key（或环境变量 GATEWAY_SECRET_KEY）与数据库一起备份，丢失后这些密文无法恢复。"
              />
              <Steps
                direction="vertical"
                size="small"
                items={[
                  {
                    title: 'AgentAPI',
                    description: '填写服务地址、API Key、构建智能体 ID 和环境 ID，用来生成应用。',
                    status: data.platform.agentapi ? 'finish' : 'process',
                  },
                  {
                    title: '灵矽平台',
                    description: '填写灵矽智控台创建的 API Key，用来为每位用户创建智能体。',
                    status: data.platform.linx ? 'finish' : 'wait',
                  },
                  {
                    title: '灵矽 MCP 地址',
                    description: '填写灵矽云能访问到的网关公网地址（以 /mcp 结尾）。MCP Token 已自动生成。',
                    status: data.platform.mcp ? 'finish' : 'wait',
                  },
                  {
                    title: '默认屏幕服务',
                    description: '填写平台托管用户使用的 Render 与 Casting 地址。',
                    status: data.platform.services ? 'finish' : 'wait',
                  },
                  {
                    title: '开放注册',
                    description: '以上都就绪并验证后，再打开注册开关。',
                    status: data.platform.registration_open ? 'finish' : 'wait',
                  },
                ]}
              />
            </section>
          ) : null}

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
                灵矽 MCP：{' '}
                {data.platform.mcp ? <Tag color="green">已配置</Tag> : <Tag color="red">未配置</Tag>}
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
