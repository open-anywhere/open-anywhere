import { ReloadOutlined } from '@ant-design/icons'
import { Alert, App as AntApp, Button, Space } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { ApiError, api } from '../../api'
import { meQueryOptions } from '../../api/queries'
import { usePageTitle } from '../../hooks/usePageTitle'
import { DevicePanel } from './components/DevicePanel'
import { PageHeader } from './components/PageHeader'

export function DevicesPage() {
  usePageTitle('设备')
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { message } = AntApp.useApp()

  const me = useQuery({
    ...meQueryOptions,
    refetchInterval: (query) =>
      query.state.data?.devices.some((device) => device.state === 'provisioning') ? 2_000 : false,
  })

  const createDevice = useMutation({
    mutationFn: api.createDevice,
    onSuccess: () => {
      message.success('正在准备你的智能体')
      queryClient.invalidateQueries({ queryKey: ['me'] })
    },
    onError: (error) => {
      message.error(error instanceof ApiError ? error.message : '暂时无法创建智能体')
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

  const devices = me.data?.devices ?? []

  return (
    <div className="page">
      <PageHeader
        eyebrow="智能体"
        title="智能体"
        description="你的语音智能体，以及它是否准备好。"
        actions={
          <Space>
            <Button
              icon={<ReloadOutlined />}
              loading={me.isFetching}
              onClick={() => me.refetch()}
            >
              刷新
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

      {devices.length ? (
        devices.map((device) => (
          <section className="surface" key={device.id}>
            <DevicePanel
              device={device}
              creating={createDevice.isPending}
              retrying={retryDevice.isPending && retryDevice.variables === device.id}
              onCreate={() => createDevice.mutate()}
              onRetry={() => retryDevice.mutate(device.id)}
              onCall={() => navigate({ to: '/portal/call', search: { device: device.id } })}
            />
          </section>
        ))
      ) : (
        <section className="surface">
          <DevicePanel
            creating={createDevice.isPending}
            retrying={false}
            onCreate={() => createDevice.mutate()}
            onRetry={() => undefined}
            onCall={() => navigate({ to: '/portal/call' })}
          />
        </section>
      )}
    </div>
  )
}
