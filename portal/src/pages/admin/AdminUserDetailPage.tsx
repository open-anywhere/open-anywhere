import { ArrowLeftOutlined, ReloadOutlined } from '@ant-design/icons'
import {
  Alert,
  App as AntApp,
  Button,
  Descriptions,
  Popconfirm,
  Space,
  Table,
  Tag,
} from 'antd'
import type { TableProps } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { ApiError, adminApi, type AdminAgent, type AdminApp, type UserStatus } from '../../api'
import { adminUserQueryOptions } from '../../api/queries'
import { AppStateTag, DeviceStateTag } from '../../components/Tags'
import { usePageTitle } from '../../hooks/usePageTitle'
import { formatDate, stepLabels } from '../../lib/format'
import { PageHeader } from '../portal/components/PageHeader'

export function AdminUserDetailPage() {
  usePageTitle('用户详情')
  const { userId } = useParams({ from: '/admin/users/$userId' })
  const queryClient = useQueryClient()
  const { message } = AntApp.useApp()
  const detail = useQuery(adminUserQueryOptions(userId))

  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: ['admin', 'user', userId] })
    queryClient.invalidateQueries({ queryKey: ['admin', 'users'] })
    queryClient.invalidateQueries({ queryKey: ['admin', 'overview'] })
  }

  const setStatus = useMutation({
    mutationFn: (next: UserStatus) => adminApi.setUserStatus(userId, next),
    onSuccess: (_data, next) => {
      message.success(next === 'disabled' ? '用户已停用' : '用户已启用')
      refresh()
    },
    onError: (error) => {
      message.error(error instanceof ApiError ? error.message : '操作失败')
    },
  })

  const retryAgent = useMutation({
    mutationFn: (id: string) => adminApi.retryAgent(id),
    onSuccess: () => {
      message.info('已重新开始开通流程')
      refresh()
      queryClient.invalidateQueries({ queryKey: ['admin', 'agents'] })
    },
    onError: (error) => {
      message.error(error instanceof ApiError ? error.message : '重试失败')
    },
  })

  const user = detail.data?.user
  const apps = detail.data?.apps ?? []

  const agentColumns: TableProps<AdminAgent>['columns'] = [
    {
      title: '智能体',
      dataIndex: 'agent_name',
      render: (_value, agent) => (
        <Space direction="vertical" size={0}>
          <strong>{agent.agent_name || '未命名智能体'}</strong>
          <span className="muted-text">
            <code className="inline-code">{agent.agent_id || '尚未分配'}</code>
          </span>
        </Space>
      ),
    },
    {
      title: 'MAC',
      dataIndex: 'mac',
      render: (value: string) => <code className="inline-code">{value}</code>,
    },
    {
      title: '状态',
      dataIndex: 'state',
      width: 100,
      render: (value: AdminAgent['state']) => <DeviceStateTag state={value} />,
    },
    {
      title: '步骤',
      dataIndex: 'step',
      width: 130,
      render: (value?: string, agent?: AdminAgent) =>
        agent?.state === 'error' && agent.error ? (
          <span className="muted-text" title={agent.error}>
            {agent.error}
          </span>
        ) : value ? (
          stepLabels[value] ?? value
        ) : (
          '—'
        ),
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      width: 150,
      render: (value: string) => formatDate(value),
    },
    {
      title: '操作',
      key: 'actions',
      width: 110,
      render: (_value, agent) => (
        <Button
          size="small"
          loading={retryAgent.isPending && retryAgent.variables === agent.id}
          onClick={() => retryAgent.mutate(agent.id)}
        >
          重试开通
        </Button>
      ),
    },
  ]

  const appColumns: TableProps<AdminApp>['columns'] = [
    {
      title: '应用',
      dataIndex: 'name',
      render: (_value, app) => (
        <Space direction="vertical" size={0}>
          <span>{app.title || app.name || '未命名应用'}</span>
          <span className="muted-text">
            <code className="inline-code">{app.app_id}</code>
          </span>
        </Space>
      ),
    },
    {
      title: '状态',
      dataIndex: 'state',
      width: 100,
      render: (value: AdminApp['state']) => <AppStateTag state={value} />,
    },
    {
      title: '构建完成',
      dataIndex: 'built_at',
      width: 160,
      render: (value?: string) => formatDate(value),
    },
    {
      title: '操作数',
      dataIndex: 'operation_count',
      width: 90,
    },
  ]

  return (
    <div className="page">
      <PageHeader
        eyebrow="Admin · User"
        title={user?.username ?? '用户详情'}
        description="账号、智能体与应用。这里不展示屏幕内容、屏幕 ID 或用户的服务地址。"
        actions={
          <Space wrap>
            <Link to="/admin/users">
              <Button icon={<ArrowLeftOutlined />}>返回用户列表</Button>
            </Link>
            <Button
              icon={<ReloadOutlined />}
              loading={detail.isFetching}
              onClick={() => detail.refetch()}
            >
              刷新
            </Button>
            {user ? (
              <Popconfirm
                title={user.status === 'active' ? '停用这个账号？' : '启用这个账号？'}
                description={
                  user.status === 'active'
                    ? '会话会立即失效，正在运行的应用会被尽力停止。'
                    : '启用后用户可以重新登录，但不会自动重新开通智能体。'
                }
                okText="确认"
                cancelText="取消"
                onConfirm={() =>
                  setStatus.mutate(user.status === 'active' ? 'disabled' : 'active')
                }
              >
                <Button danger={user.status === 'active'} loading={setStatus.isPending}>
                  {user.status === 'active' ? '停用账号' : '启用账号'}
                </Button>
              </Popconfirm>
            ) : null}
          </Space>
        }
      />

      {detail.error ? (
        <Alert
          type="error"
          showIcon
          className="panel-alert"
          message="无法加载用户详情"
          description={detail.error instanceof ApiError ? detail.error.message : String(detail.error)}
        />
      ) : null}

      {user ? (
        <>
          <section className="surface settings-section">
            <div className="section-header">
              <div>
                <h2>账号</h2>
                <p>停用账号会让会话立即失效，并拒绝新的 MCP 连接。</p>
              </div>
            </div>
            <Descriptions
              size="small"
              column={{ xs: 1, sm: 2 }}
              items={[
                { key: 'username', label: '用户名', children: user.username },
                { key: 'id', label: '用户 ID', children: <code className="inline-code">{user.id}</code> },
                {
                  key: 'role',
                  label: '角色',
                  children: user.role === 'admin' ? <Tag color="gold">管理员</Tag> : '普通用户',
                },
                {
                  key: 'status',
                  label: '状态',
                  children:
                    user.status === 'active' ? (
                      <Tag color="green">启用</Tag>
                    ) : (
                      <Tag color="red">停用</Tag>
                    ),
                },
                { key: 'created', label: '注册时间', children: formatDate(user.created_at) },
                { key: 'login', label: '最后登录', children: formatDate(user.last_login_at) },
                { key: 'apps', label: '应用数量', children: user.app_count },
              ]}
            />
          </section>

          <section className="surface surface-flush table-surface">
            <div className="section-header" style={{ padding: 20 }}>
              <div>
                <h2>智能体</h2>
                <p>注册后自动创建。开通失败时可以在这里重试。</p>
              </div>
            </div>
            <Table<AdminAgent>
              rowKey="id"
              size="middle"
              pagination={false}
              columns={agentColumns}
              dataSource={user.agents}
              locale={{ emptyText: '尚未创建智能体' }}
            />
          </section>

          <section className="surface surface-flush table-surface">
            <div className="section-header" style={{ padding: 20 }}>
              <div>
                <h2>应用</h2>
                <p>只显示状态与构建信息，不包含屏幕字段或用户的提示词。</p>
              </div>
            </div>
            <Table<AdminApp>
              rowKey="app_id"
              size="middle"
              pagination={false}
              columns={appColumns}
              dataSource={apps}
              locale={{ emptyText: '还没有应用' }}
            />
          </section>
        </>
      ) : null}
    </div>
  )
}
