import { ReloadOutlined, SearchOutlined } from '@ant-design/icons'
import { Alert, Button, Input, Select, Space, Table } from 'antd'
import type { TableProps } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { ApiError, type AdminApp } from '../../api'
import { adminAppsQueryOptions } from '../../api/queries'
import { AppStateTag } from '../../components/Tags'
import { usePageTitle } from '../../hooks/usePageTitle'
import { formatDate } from '../../lib/format'
import { PageHeader } from '../portal/components/PageHeader'

const stateOptions = [
  { value: 'all', label: '全部状态' },
  { value: 'building', label: '构建中' },
  { value: 'running', label: '运行中' },
  { value: 'idle', label: '待启动' },
  { value: 'error', label: '失败' },
]

export function AdminAppsPage() {
  usePageTitle('应用管理')
  const [query, setQuery] = useState('')
  const [state, setState] = useState('all')
  const apps = useQuery(
    adminAppsQueryOptions({
      query: query.trim(),
      state: state === 'all' ? '' : state,
      limit: 200,
    }),
  )

  const columns: TableProps<AdminApp>['columns'] = [
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
      title: '所属用户',
      dataIndex: 'username',
      width: 160,
      render: (_value, app) =>
        app.user_id ? (
          <Link to="/admin/users/$userId" params={{ userId: app.user_id }} className="text-link">
            {app.username || app.user_id}
          </Link>
        ) : (
          <span className="muted-text">未知用户</span>
        ),
    },
    {
      title: '智能体',
      dataIndex: 'agent_id',
      width: 180,
      render: (value?: string) =>
        value ? <code className="inline-code">{value}</code> : <span className="muted-text">—</span>,
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
        eyebrow="Admin"
        title="应用"
        description="全平台应用与状态。管理端不展示屏幕、投屏目标或用户的提示词内容。"
        actions={
          <Button icon={<ReloadOutlined />} loading={apps.isFetching} onClick={() => apps.refetch()}>
            刷新
          </Button>
        }
      />

      {apps.error ? (
        <Alert
          type="error"
          showIcon
          className="panel-alert"
          message="无法加载应用列表"
          description={apps.error instanceof ApiError ? apps.error.message : String(apps.error)}
        />
      ) : null}

      <div className="toolbar">
        <Input.Search
          allowClear
          prefix={<SearchOutlined />}
          placeholder="搜索应用、用户或智能体"
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
        <span className="toolbar-count">{apps.data?.apps.length ?? 0} 个应用</span>
      </div>

      <section className="surface surface-flush table-surface">
        <Table<AdminApp>
          rowKey="app_id"
          size="middle"
          loading={apps.isPending}
          columns={columns}
          dataSource={apps.data?.apps ?? []}
          scroll={{ x: 900 }}
          pagination={{ pageSize: 20, showSizeChanger: false }}
        />
      </section>
    </div>
  )
}
