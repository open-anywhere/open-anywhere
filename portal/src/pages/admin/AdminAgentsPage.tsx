import { ReloadOutlined, SearchOutlined } from '@ant-design/icons'
import {
  Alert,
  App as AntApp,
  Button,
  Input,
  Select,
  Space,
  Table,
} from 'antd'
import type { TableProps } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { ApiError, adminApi, type AdminAgent } from '../../api'
import { adminAgentsQueryOptions } from '../../api/queries'
import { DeviceStateTag } from '../../components/Tags'
import { usePageTitle } from '../../hooks/usePageTitle'
import { formatDate, stepLabels } from '../../lib/format'
import { PageHeader } from '../portal/components/PageHeader'

const stateOptions = [
  { value: 'all', label: '全部状态' },
  { value: 'provisioning', label: '开通中' },
  { value: 'registered', label: '已注册' },
  { value: 'error', label: '失败' },
  { value: 'disabled', label: '已停用' },
]

export function AdminAgentsPage() {
  usePageTitle('智能体管理')
  const [query, setQuery] = useState('')
  const [state, setState] = useState('all')
  const queryClient = useQueryClient()
  const { message } = AntApp.useApp()

  const agents = useQuery(
    adminAgentsQueryOptions({
      query: query.trim(),
      state: state === 'all' ? '' : state,
    }),
  )

  const retry = useMutation({
    mutationFn: (id: string) => adminApi.retryAgent(id),
    onSuccess: () => {
      message.info('已重新开始开通流程')
      queryClient.invalidateQueries({ queryKey: ['admin', 'agents'] })
      queryClient.invalidateQueries({ queryKey: ['admin', 'overview'] })
    },
    onError: (error) => {
      message.error(error instanceof ApiError ? error.message : '重试失败')
    },
  })

  const columns: TableProps<AdminAgent>['columns'] = [
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
      title: '所属用户',
      dataIndex: 'username',
      width: 160,
      render: (_value, agent) => (
        <Link to="/admin/users/$userId" params={{ userId: agent.user_id }} className="text-link">
          {agent.username || agent.user_id}
        </Link>
      ),
    },
    {
      title: 'MAC',
      dataIndex: 'mac',
      width: 160,
      render: (value: string) => <code className="inline-code">{value}</code>,
    },
    {
      title: '状态',
      dataIndex: 'state',
      width: 100,
      render: (value: AdminAgent['state']) => <DeviceStateTag state={value} />,
    },
    {
      title: '步骤 / 错误',
      dataIndex: 'step',
      width: 180,
      render: (_value, agent) =>
        agent.state === 'error' && agent.error ? (
          <span className="muted-text" title={agent.error}>
            {agent.error}
          </span>
        ) : agent.step ? (
          stepLabels[agent.step] ?? agent.step
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
          loading={retry.isPending && retry.variables === agent.id}
          onClick={() => retry.mutate(agent.id)}
        >
          重试开通
        </Button>
      ),
    },
  ]

  return (
    <div className="page">
      <PageHeader
        eyebrow="Admin"
        title="智能体"
        description="所有用户的虚拟 Linx 设备与开通状态。开通失败时可以重试。"
        actions={
          <Button
            icon={<ReloadOutlined />}
            loading={agents.isFetching}
            onClick={() => agents.refetch()}
          >
            刷新
          </Button>
        }
      />

      {agents.error ? (
        <Alert
          type="error"
          showIcon
          className="panel-alert"
          message="无法加载智能体列表"
          description={agents.error instanceof ApiError ? agents.error.message : String(agents.error)}
        />
      ) : null}

      <div className="toolbar">
        <Input.Search
          allowClear
          prefix={<SearchOutlined />}
          placeholder="搜索用户名、MAC 或智能体 ID"
          className="toolbar-search"
          onSearch={(value) => setQuery(value)}
          enterButton
        />
        <Select
          value={state}
          className="toolbar-select"
          aria-label="按状态筛选"
          onChange={setState}
          options={stateOptions}
        />
        <span className="toolbar-count">{agents.data?.agents.length ?? 0} 个智能体</span>
      </div>

      <section className="surface surface-flush table-surface">
        <Table<AdminAgent>
          rowKey="id"
          size="middle"
          loading={agents.isPending}
          columns={columns}
          dataSource={agents.data?.agents ?? []}
          scroll={{ x: 960 }}
          pagination={{ pageSize: 20, showSizeChanger: false }}
        />
      </section>
    </div>
  )
}
