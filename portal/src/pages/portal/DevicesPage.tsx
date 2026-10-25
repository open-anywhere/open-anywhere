import { AudioOutlined, DeleteOutlined, DisconnectOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { Alert, App as AntApp, Button, Empty, Popconfirm, Space, Table, Tag } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { ApiError, api, type CreateDeviceInput, type Device } from '../../api'
import { meQueryOptions } from '../../api/queries'
import { DeviceStateTag, VoiceTag } from '../../components/Tags'
import { useDeviceConnection } from '../../hooks/useDeviceConnection'
import { usePageTitle } from '../../hooks/usePageTitle'
import { formatDate } from '../../lib/format'
import { AddDeviceModal } from './components/AddDeviceModal'
import { PageHeader } from './components/PageHeader'

export function DevicesPage() {
  usePageTitle('设备')
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { message, modal } = AntApp.useApp()
  const [addOpen, setAddOpen] = useState(false)

  const me = useQuery({
    ...meQueryOptions,
    refetchInterval: (query) =>
      query.state.data?.devices.some((device) => device.state === 'provisioning') ? 2_000 : false,
  })

  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: ['me'] })
    queryClient.invalidateQueries({ queryKey: ['apps'] })
  }

  const createDevice = useMutation({
    mutationFn: (input?: CreateDeviceInput) => api.createDevice(input),
    onSuccess: (_, input) => {
      setAddOpen(false)
      message.success(input?.kind === 'physical' ? '设备已绑定' : '正在准备你的智能体')
      refresh()
    },
    onError: (error) => {
      message.error(error instanceof ApiError ? error.message : '暂时无法添加设备')
    },
  })

  const removeDevice = useMutation({
    mutationFn: ({ id, force }: { id: string; force?: boolean }) => api.deleteDevice(id, force),
    onSuccess: () => {
      message.success('设备已移除')
      refresh()
    },
    onError: (error, variables) => {
      const text = error instanceof ApiError ? error.message : '移除失败'
      if (error instanceof ApiError && error.code === 'UNBIND_FAILED' && !variables.force) {
        modal.confirm({
          title: '向灵矽解绑失败',
          content: `${text}。仍要从这里移除吗？`,
          okText: '强制移除',
          okButtonProps: { danger: true },
          cancelText: '取消',
          onOk: () => removeDevice.mutate({ id: variables.id, force: true }),
        })
        return
      }
      message.error(text)
    },
  })

  const retryDevice = useMutation({
    mutationFn: (id: string) => api.retryDevice(id),
    onSuccess: () => {
      message.info('已重新开始准备')
      queryClient.invalidateQueries({ queryKey: ['me'] })
    },
    onError: (error) => {
      message.error(error instanceof ApiError ? error.message : '重试失败')
    },
  })

  const { disconnect, reconnect } = useDeviceConnection()
  const devices = me.data?.devices ?? []

  const actions = (device: Device) => {
    const hardware = device.kind === 'physical'
    const registered = device.state === 'registered'
    const offline = !hardware && device.voice.state === 'offline'
    return (
      <Space wrap size="small">
        {registered && !hardware ? (
          <Button
            size="small"
            type="primary"
            icon={<AudioOutlined />}
            disabled={offline}
            onClick={() => navigate({ to: '/portal/call', search: { device: device.id } })}
          >
            对话
          </Button>
        ) : null}
        {registered && !hardware ? (
          <Button
            size="small"
            icon={<ReloadOutlined />}
            loading={reconnect.isPending && reconnect.variables === device.id}
            onClick={() => reconnect.mutate(device.id)}
          >
            重新连接
          </Button>
        ) : null}
        {registered && !hardware && !offline ? (
          <Popconfirm
            title="断开与灵矽的连接？"
            description="正在进行的对话会中断，之后可以随时重新连接。"
            okText="断开"
            cancelText="取消"
            onConfirm={() => disconnect.mutate(device.id)}
          >
            <Button
              size="small"
              icon={<DisconnectOutlined />}
              loading={disconnect.isPending && disconnect.variables === device.id}
            >
              断开
            </Button>
          </Popconfirm>
        ) : null}
        {device.state === 'error' ? (
          <Button
            size="small"
            icon={<ReloadOutlined />}
            loading={retryDevice.isPending && retryDevice.variables === device.id}
            onClick={() => retryDevice.mutate(device.id)}
          >
            重试
          </Button>
        ) : null}
        {devices.length > 1 ? (
          <Popconfirm
            title={hardware ? '移除这台硬件设备？' : '移除这个虚拟设备？'}
            description={
              hardware
                ? '会在灵矽上解绑它，之后可以用新的激活码重新添加。'
                : '浏览器里的语音连接会关闭。它名下还有应用时需要先删除应用。'
            }
            okText="移除"
            cancelText="取消"
            okButtonProps={{ danger: true }}
            onConfirm={() => removeDevice.mutate({ id: device.id })}
          >
            <Button
              size="small"
              danger
              icon={<DeleteOutlined />}
              loading={removeDevice.isPending && removeDevice.variables?.id === device.id}
            >
              移除
            </Button>
          </Popconfirm>
        ) : null}
      </Space>
    )
  }

  return (
    <div className="page">
      <PageHeader
        eyebrow="Devices"
        title="设备"
        description="用来和智能体说话、把应用投到屏幕上的设备。浏览器里的虚拟设备和硬件设备都在这里管理。"
        actions={
          <Space>
            <Button icon={<ReloadOutlined />} loading={me.isFetching} onClick={() => me.refetch()}>
              刷新
            </Button>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => setAddOpen(true)}>
              添加设备
            </Button>
          </Space>
        }
      />

      {me.error && !(me.error instanceof ApiError && me.error.status === 401) ? (
        <Alert
          type="error"
          showIcon
          className="panel-alert"
          message="无法加载设备信息"
          description={me.error.message}
        />
      ) : null}

      <section className="surface">
        <Table<Device>
          rowKey="id"
          size="middle"
          loading={me.isPending}
          dataSource={devices}
          pagination={false}
          scroll={{ x: 'max-content' }}
          locale={{
            emptyText: (
              <Empty
                description="还没有设备。添加一个虚拟设备，就能在浏览器里和智能体说话。"
                image={Empty.PRESENTED_IMAGE_SIMPLE}
              />
            ),
          }}
          columns={[
            {
              title: '设备',
              key: 'name',
              render: (_, device) => (
                <div>
                  <strong>{device.name || (device.kind === 'physical' ? '硬件设备' : '虚拟设备')}</strong>
                  {device.kind === 'physical' ? <div className="muted-text">{device.mac}</div> : null}
                </div>
              ),
            },
            {
              title: '类型',
              key: 'kind',
              render: (_, device) => <Tag>{device.kind === 'physical' ? '硬件' : '虚拟'}</Tag>,
            },
            {
              title: '智能体',
              key: 'agent',
              render: (_, device) =>
                device.agent_id ? <Link to="/portal/agents">{device.agent_name || '智能体'}</Link> : '—',
            },
            {
              title: '状态',
              key: 'state',
              render: (_, device) => (
                <Space size="small" wrap>
                  <DeviceStateTag state={device.state} />
                  {device.kind === 'virtual' && device.state === 'registered' ? (
                    <VoiceTag state={device.voice.state} connected={device.voice.connected} />
                  ) : null}
                </Space>
              ),
            },
            { title: '添加于', key: 'created', render: (_, device) => formatDate(device.created_at) },
            { title: '操作', key: 'actions', render: (_, device) => actions(device) },
          ]}
        />
      </section>

      <AddDeviceModal
        open={addOpen}
        devices={devices}
        submitting={createDevice.isPending}
        onCancel={() => setAddOpen(false)}
        onSubmit={(input) => createDevice.mutate(input)}
      />
    </div>
  )
}
