import { ReloadOutlined, SearchOutlined } from '@ant-design/icons'
import {
  Alert,
  App as AntApp,
  Button,
  Input,
  Popconfirm,
  Select,
  Space,
  Table,
  Tag,
} from 'antd'
import type { TableProps } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { ApiError, adminApi, type AdminUser, type UserStatus } from '../../api'
import { adminUsersQueryOptions, meQueryOptions } from '../../api/queries'
import { usePageTitle } from '../../hooks/usePageTitle'
import { formatDate } from '../../lib/format'
import { PageHeader } from '../portal/components/PageHeader'

const limit = 50

export function AdminUsersPage() {
  usePageTitle('用户管理')
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState<'all' | UserStatus>('all')
  const [offset, setOffset] = useState(0)
  const queryClient = useQueryClient()
  const { message } = AntApp.useApp()

  const me = useQuery(meQueryOptions)
  const users = useQuery(
    adminUsersQueryOptions({
      query: query.trim(),
      status: status === 'all' ? '' : status,
      limit,
      offset,
    }),
  )

  const setUserStatus = useMutation({
    mutationFn: ({ id, next }: { id: string; next: UserStatus }) =>
      adminApi.setUserStatus(id, next),
    onSuccess: (_data, variables) => {
      message.success(variables.next === 'disabled' ? '用户已停用' : '用户已启用')
      queryClient.invalidateQueries({ queryKey: ['admin', 'users'] })
      queryClient.invalidateQueries({ queryKey: ['admin', 'user', variables.id] })
      queryClient.invalidateQueries({ queryKey: ['admin', 'overview'] })
    },
    onError: (error) => {
      message.error(error instanceof ApiError ? error.message : '操作失败')
    },
  })

  const columns: TableProps<AdminUser>['columns'] = [
    {
      title: '用户',
      dataIndex: 'username',
      render: (_value, user) => (
        <Space direction="vertical" size={0}>
          <Link to="/admin/users/$userId" params={{ userId: user.id }} className="text-link">
            {user.username}
          </Link>
          <span className="muted-text">
            <code className="inline-code">{user.id}</code>
          </span>
        </Space>
      ),
    },
    {
      title: '角色',
      dataIndex: 'role',
      width: 90,
      render: (role: AdminUser['role']) =>
        role === 'admin' ? <Tag color="gold">管理员</Tag> : <Tag>普通用户</Tag>,
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 90,
      render: (value: UserStatus) =>
        value === 'active' ? <Tag color="green">启用</Tag> : <Tag color="red">停用</Tag>,
    },
    {
      title: '智能体',
      dataIndex: 'agents',
      render: (_value, user) =>
        user.agents.length ? (
          <Space direction="vertical" size={0}>
            {user.agents.map((agent) => (
              <span key={agent.id}>
                {agent.agent_name || agent.mac}{' '}
                <Tag color={agent.state === 'registered' ? 'green' : agent.state === 'error' ? 'red' : 'blue'}>
                  {agent.state}
                </Tag>
              </span>
            ))}
          </Space>
        ) : (
          <span className="muted-text">未创建</span>
        ),
    },
    {
      title: '应用',
      dataIndex: 'app_count',
      width: 70,
      render: (value: number) => value || '—',
    },
    {
      title: '注册时间',
      dataIndex: 'created_at',
      width: 150,
      render: (value: string) => formatDate(value),
    },
    {
      title: '最后登录',
      dataIndex: 'last_login_at',
      width: 150,
      render: (value?: string) => formatDate(value),
    },
    {
      title: '操作',
      key: 'actions',
      width: 180,
      render: (_value, user) => {
        const self = me.data?.user.id === user.id
        const next: UserStatus = user.status === 'active' ? 'disabled' : 'active'
        return (
          <Space>
            <Link to="/admin/users/$userId" params={{ userId: user.id }}>
              <Button size="small">详情</Button>
            </Link>
            <Popconfirm
              title={next === 'disabled' ? '停用这个账号？' : '启用这个账号？'}
              description={
                next === 'disabled'
                  ? '会话会立即失效，正在运行的应用会被尽力停止。'
                  : '启用后用户可以重新登录，但不会自动重新开通智能体。'
              }
              okText="确认"
              cancelText="取消"
              disabled={self}
              onConfirm={() => setUserStatus.mutate({ id: user.id, next })}
            >
              <Button
                size="small"
                danger={next === 'disabled'}
                disabled={self}
                loading={setUserStatus.isPending && setUserStatus.variables?.id === user.id}
              >
                {next === 'disabled' ? '停用' : '启用'}
              </Button>
            </Popconfirm>
          </Space>
        )
      },
    },
  ]

  return (
    <div className="page">
      <PageHeader
        eyebrow="Admin"
        title="用户"
        description="管理账号状态，查看每个用户的智能体与应用数量。管理员不能查看用户的屏幕内容。"
        actions={
          <Button
            icon={<ReloadOutlined />}
            loading={users.isFetching}
            onClick={() => users.refetch()}
          >
            刷新
          </Button>
        }
      />

      {users.error ? (
        <Alert
          type="error"
          showIcon
          className="panel-alert"
          message="无法加载用户列表"
          description={users.error instanceof ApiError ? users.error.message : String(users.error)}
        />
      ) : null}

      <div className="toolbar">
        <Input.Search
          allowClear
          prefix={<SearchOutlined />}
          placeholder="搜索用户名"
          className="toolbar-search"
          onSearch={(value) => {
            setQuery(value)
            setOffset(0)
          }}
          enterButton
        />
        <Select
          value={status}
          className="toolbar-select"
          aria-label="按状态筛选"
          onChange={(value) => {
            setStatus(value)
            setOffset(0)
          }}
          options={[
            { value: 'all', label: '全部状态' },
            { value: 'active', label: '启用' },
            { value: 'disabled', label: '停用' },
          ]}
        />
        <span className="toolbar-count">{users.data?.total ?? 0} 个用户</span>
      </div>

      <section className="surface surface-flush table-surface">
        <Table<AdminUser>
          rowKey="id"
          size="middle"
          loading={users.isPending}
          columns={columns}
          dataSource={users.data?.users ?? []}
          scroll={{ x: 960 }}
          pagination={{
            current: Math.floor(offset / limit) + 1,
            pageSize: limit,
            total: users.data?.total ?? 0,
            showSizeChanger: false,
            onChange: (page) => setOffset((page - 1) * limit),
          }}
        />
      </section>
    </div>
  )
}
