import { ReloadOutlined, ShopOutlined } from '@ant-design/icons'
import { App as AntApp, Button, Form, Input, Modal, Select, Skeleton, Space, Tag } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { ApiError, api, type StoreItem } from '../../api'
import { meQueryOptions } from '../../api/queries'
import { EmptyState } from '../../components/EmptyState'
import { usePageTitle } from '../../hooks/usePageTitle'
import { PageHeader } from './components/PageHeader'

interface InstallValues {
  name: string
  device_id: string
}

export function StorePage() {
  usePageTitle('应用商城')
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { message } = AntApp.useApp()
  const [search, setSearch] = useState('')
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState<StoreItem | null>(null)
  const [form] = Form.useForm<InstallValues>()

  const me = useQuery(meQueryOptions)
  const store = useQuery({
    queryKey: ['store', query],
    queryFn: () => api.listStore(query),
  })

  const devices = (me.data?.devices ?? []).filter((device) => device.state === 'registered' && device.agent_id)

  const install = useMutation({
    mutationFn: (values: InstallValues) =>
      api.cloneStoreApp(selected?.store_id ?? '', { name: values.name, device_id: values.device_id }),
    onSuccess: async (result) => {
      await queryClient.invalidateQueries({ queryKey: ['apps'] })
      message.success('已添加到你的应用，可以直接开始')
      setSelected(null)
      form.resetFields()
      navigate({ to: '/portal/apps/$appId', params: { appId: result.app_id } })
    },
    onError: (error) => {
      message.error(error instanceof ApiError ? error.message : '添加失败，请稍后重试')
    },
  })

  const items = store.data?.items ?? []

  return (
    <div className="page">
      <PageHeader
        eyebrow="Store"
        title="应用商城"
        description="这里是从大家的应用里公开出来、经过审核的作品。添加一份，就会复制成你自己的应用。"
        actions={
          <Space>
            <Input.Search
              allowClear
              value={search}
              placeholder="搜索标题或简介"
              prefix={<ShopOutlined />}
              onChange={(event) => setSearch(event.target.value)}
              onSearch={(value) => setQuery(value.trim())}
              style={{ width: 240 }}
            />
            <Button icon={<ReloadOutlined />} onClick={() => store.refetch()} loading={store.isFetching}>
              刷新
            </Button>
          </Space>
        }
      />

      {store.isLoading ? (
        <Skeleton active />
      ) : items.length === 0 ? (
        <EmptyState
          title="商城里还没有应用"
          description="别人公开并通过审核的应用会出现在这里。你也可以在自己的应用里点「公开到商城」。"
        />
      ) : (
        <div className="stat-grid">
          {items.map((item) => (
            <section className="surface" key={item.store_id}>
              <div className="section-header">
                <div>
                  <h2>{item.title}</h2>
                  <p>{item.summary || '这个应用还没有介绍。'}</p>
                </div>
                <Tag>{item.operation_count} 个操作</Tag>
              </div>
              <Space wrap>
                <Button
                  type="primary"
                  onClick={() => {
                    setSelected(item)
                    form.setFieldsValue({
                      name: item.title,
                      device_id: devices.length === 1 ? devices[0].id : undefined,
                    })
                  }}
                >
                  添加到我的应用
                </Button>
              </Space>
            </section>
          ))}
        </div>
      )}

      <Modal
        open={selected !== null}
        title={selected ? `添加「${selected.title}」` : '添加应用'}
        okText="添加"
        cancelText="取消"
        confirmLoading={install.isPending}
        onOk={() => form.submit()}
        onCancel={() => {
          setSelected(null)
          form.resetFields()
        }}
      >
        <Form form={form} layout="vertical" onFinish={(values) => install.mutate(values)}>
          <Form.Item
            label="应用名字"
            name="name"
            rules={[{ required: true, message: '给你的应用起个名字' }]}
          >
            <Input maxLength={40} placeholder="例如：周末答题夜" />
          </Form.Item>
          <Form.Item
            label="装到哪台设备"
            name="device_id"
            rules={[{ required: true, message: '选择一台已完成开通的设备' }]}
            extra="应用属于设备：在哪台设备上添加，就由哪台设备的智能体使用。"
          >
            <Select
              options={devices.map((device) => ({
                value: device.id,
                label: device.name || device.mac,
              }))}
              placeholder={devices.length ? '选择设备' : '还没有可用的设备'}
              disabled={devices.length === 0}
            />
          </Form.Item>
          <p className="muted-text">
            添加后会复制一份独立的产物，可以改名、启动和投屏，但不能修改内容。
          </p>
        </Form>
      </Modal>
    </div>
  )
}
