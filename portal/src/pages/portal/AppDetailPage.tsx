import {
  ArrowLeftOutlined,
  DeleteOutlined,
  DownloadOutlined,
  FileTextOutlined,
} from '@ant-design/icons'
import {
  Alert,
  App as AntApp,
  Button,
  Card,
  Descriptions,
  Modal,
  Popconfirm,
  Result,
  Skeleton,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd'
import type { TableProps } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from '@tanstack/react-router'
import { useState } from 'react'
import { ApiError, api, type Operation } from '../../api'
import { appQueryOptions, meQueryOptions, screensQueryOptions } from '../../api/queries'
import { AppStateTag } from '../../components/Tags'
import { usePageTitle } from '../../hooks/usePageTitle'
import { deviceForApp } from '../../lib/device'
import { appStateLabels, formatDate, formatDuration } from '../../lib/format'
import { PageHeader } from './components/PageHeader'

function methodColor(method: string): string {
  switch (method.toUpperCase()) {
    case 'GET':
      return 'blue'
    case 'POST':
      return 'green'
    case 'PUT':
    case 'PATCH':
      return 'gold'
    case 'DELETE':
      return 'red'
    default:
      return 'default'
  }
}

const columns: TableProps<Operation>['columns'] = [
  {
    title: '方法',
    dataIndex: 'method',
    width: 92,
    render: (method: string) => <Tag color={methodColor(method)}>{method.toUpperCase()}</Tag>,
  },
  {
    title: '路径',
    dataIndex: 'path',
    render: (path: string) => <code className="inline-code">{path}</code>,
  },
  {
    title: '说明',
    dataIndex: 'summary',
    render: (summary?: string, record?: Operation) => summary || record?.description || '—',
  },
]

export function AppDetailPage() {
  const { appId } = useParams({ from: '/portal/apps/$appId' })
  usePageTitle('应用详情')
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { message } = AntApp.useApp()
  const [openapiOpen, setOpenapiOpen] = useState(false)

  const app = useQuery({
    ...appQueryOptions(appId),
    refetchInterval: (query) => (query.state.data?.app.state === 'building' ? 3_000 : false),
  })
  const me = useQuery(meQueryOptions)
  const screens = useQuery(screensQueryOptions)

  const openapi = useQuery({
    queryKey: ['openapi', appId],
    queryFn: () => api.openapi(appId),
    enabled: openapiOpen && app.data?.app.openapi_available === true,
    retry: false,
  })

  const remove = useMutation({
    mutationFn: () => api.deleteApp(appId),
    onSuccess: () => {
      queryClient.removeQueries({ queryKey: ['app', appId] })
      queryClient.removeQueries({ queryKey: ['openapi', appId] })
      queryClient.invalidateQueries({ queryKey: ['apps'] })
      queryClient.invalidateQueries({ queryKey: ['screens'] })
      message.success('应用已删除')
      navigate({ to: '/portal/apps' })
    },
    onError: (error) => {
      message.error(error instanceof ApiError ? error.message : '删除失败')
    },
  })

  if (app.isPending) {
    return (
      <div className="page">
        <section className="surface">
          <Skeleton active paragraph={{ rows: 8 }} />
        </section>
      </div>
    )
  }

  if (app.error) {
    const notFound = app.error instanceof ApiError && app.error.status === 404
    return (
      <div className="page">
        <Result
          status={notFound ? '404' : 'error'}
          title={notFound ? '应用不存在' : '无法加载应用'}
          subTitle={app.error.message}
          extra={
            <Button type="primary" onClick={() => navigate({ to: '/portal/apps' })}>
              返回应用列表
            </Button>
          }
        />
      </div>
    )
  }

  const detail = app.data.app
  const title = detail.title || detail.name || '未命名应用'
  const building = detail.state === 'building'
  const device = deviceForApp(me.data?.devices, detail)
  const screen = screens.data?.screens.find((item) => item.screen_id === detail.screen_id)

  return (
    <div className="page">
      <Link to="/portal/apps" className="back-link">
        <ArrowLeftOutlined /> 返回应用
      </Link>

      <PageHeader
        eyebrow="Application"
        title={title}
        description={detail.summary || detail.message || '这个应用还没有描述。'}
        actions={
          <Space wrap>
            <AppStateTag state={detail.state} />
            {detail.openapi_available ? (
              <Button icon={<FileTextOutlined />} onClick={() => setOpenapiOpen(true)}>
                接口文档
              </Button>
            ) : null}
            <Popconfirm
              title="删除这个应用？"
              description="它会从屏幕上移除，相关记录也会一起清理，且无法撤销。"
              okText="删除"
              cancelText="取消"
              okButtonProps={{ danger: true, loading: remove.isPending }}
              disabled={building}
              onConfirm={() => remove.mutate()}
            >
              <Button danger icon={<DeleteOutlined />} disabled={building}>
                删除
              </Button>
            </Popconfirm>
          </Space>
        }
      />

      {building ? (
        <Alert
          type="info"
          showIcon
          className="panel-alert"
          message="正在生成应用"
          description={detail.progress || '生成完成前，详细内容还看不到。页面会自动刷新。'}
        />
      ) : null}

      {detail.state === 'error' && detail.error ? (
        <Alert
          type="error"
          showIcon
          className="panel-alert"
          message={
            detail.error.stage === 'building'
              ? '生成失败'
              : detail.error.stage === 'starting'
                ? '播放失败'
                : '播放中断'
          }
          description={
            detail.error.message || detail.error.last_agent_message || detail.error.code || '未知错误'
          }
        />
      ) : null}

      <section className="surface">
        <div className="section-header">
          <div>
            <h2>应用状态</h2>
            <p>从生成到上屏，这个应用经历了什么。</p>
          </div>
        </div>
        <Descriptions
          size="small"
          column={{ xs: 1, sm: 2, lg: 3 }}
          items={[
            {
              key: 'device',
              label: '智能体',
              children: device ? device.agent_name || '你的智能体' : '没有关联的智能体',
            },
            {
              key: 'state',
              label: '状态',
              children: appStateLabels[detail.state] ?? detail.state,
            },
            {
              key: 'screen',
              label: '屏幕',
              children: screen ? (
                <Link to="/portal/screens" className="text-link">
                  {screen.name}
                </Link>
              ) : detail.screen_id ? (
                '屏幕（暂不可见）'
              ) : (
                '—'
              ),
            },
            { key: 'build_started', label: '开始生成', children: formatDate(detail.build_started_at) },
            { key: 'built', label: '生成完成', children: formatDate(detail.built_at) },
            {
              key: 'duration',
              label: '生成耗时',
              children: formatDuration(detail.build_duration_seconds),
            },
            { key: 'started', label: '开始播放', children: formatDate(detail.started_at) },
            { key: 'stopped', label: '停止播放', children: formatDate(detail.stopped_at) },
            {
              key: 'uptime',
              label: '已播放',
              children: detail.state === 'running' ? formatDuration(detail.uptime_seconds) : '—',
            },
            { key: 'operations', label: '动作数', children: `${detail.operation_count} 个` },
          ]}
        />
        {detail.message ? (
          <Typography.Paragraph type="secondary" className="detail-message">
            最近一条消息：{detail.message}
          </Typography.Paragraph>
        ) : null}
      </section>

      <section className="surface">
        <div className="section-header">
          <div>
            <h2>
              它能做什么
              <span className="count">{detail.operations.length}</span>
            </h2>
            <p>这个应用可以响应的动作。</p>
          </div>
          {detail.openapi_available ? (
            <Space>
              <Button size="small" icon={<FileTextOutlined />} onClick={() => setOpenapiOpen(true)}>
                查看接口文档
              </Button>
              <Button
                size="small"
                icon={<DownloadOutlined />}
                href={api.openapiDownloadURL(appId)}
                download="openapi.yaml"
              >
                下载
              </Button>
            </Space>
          ) : null}
        </div>
        <Card className="table-surface" styles={{ body: { padding: 0 } }}>
          {detail.operations.length ? (
            <Table<Operation>
              size="small"
              rowKey="operation_id"
              pagination={false}
              columns={columns}
              dataSource={detail.operations}
              scroll={{ x: true }}
            />
          ) : (
            <div className="empty-table">
              {building ? '生成完成后会列出它能做什么。' : '这个应用还没有可以执行的动作。'}
            </div>
          )}
        </Card>
      </section>

      <Modal
        open={openapiOpen}
        title="接口文档"
        width={860}
        footer={[
          <Button key="close" onClick={() => setOpenapiOpen(false)}>
            关闭
          </Button>,
          <Button
            key="download"
            type="primary"
            icon={<DownloadOutlined />}
            href={api.openapiDownloadURL(appId)}
            download="openapi.yaml"
          >
            下载 YAML
          </Button>,
        ]}
        onCancel={() => setOpenapiOpen(false)}
      >
        {openapi.isPending ? (
          <Skeleton active paragraph={{ rows: 8 }} />
        ) : openapi.error ? (
          <Alert
            type="error"
            showIcon
            message="暂时无法读取接口文档"
            description={openapi.error.message}
          />
        ) : (
          <pre className="code-panel openapi-panel">{openapi.data}</pre>
        )}
      </Modal>
    </div>
  )
}
