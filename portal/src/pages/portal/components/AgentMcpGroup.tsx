import { App as AntApp, Checkbox, Divider, Skeleton, Tag } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ApiError, api } from '../../../api'

/** MCP services on one agent. Choices are saved as soon as they are made. */
export function AgentMcpGroup({ deviceId }: { deviceId: string }) {
  const { message } = AntApp.useApp()
  const queryClient = useQueryClient()
  const queryKey = ['device-mcps', deviceId]
  const services = useQuery({ queryKey, queryFn: () => api.getDeviceMcps(deviceId), staleTime: 0, retry: false })

  const save = useMutation({
    mutationFn: (ids: string[]) => api.setDeviceMcps(deviceId, ids),
    onSuccess: () => {
      message.success('MCP 服务已更新。要让它生效，请重新连接智能体，硬件设备需要重启')
      queryClient.invalidateQueries({ queryKey })
    },
    onError: (error) => message.error(error instanceof ApiError ? error.message : '保存失败'),
  })

  const data = services.data
  const toggle = (id: string, checked: boolean) => {
    const chosen = (data?.available ?? []).filter((item) => item.selected).map((item) => item.id)
    save.mutate(checked ? [...chosen, id] : chosen.filter((value) => value !== id))
  }

  return (
    <>
      <Divider titlePlacement="start" plain>
        MCP 服务
      </Divider>
      {services.isPending ? (
        <Skeleton active paragraph={{ rows: 1 }} />
      ) : services.isError || !data ? (
        <p className="muted-text">暂时无法读取 MCP 服务。</p>
      ) : (
        <div style={{ display: 'grid', gap: 8 }}>
          {data.required ? (
            <div>
              <Checkbox checked disabled>
                {data.required.name}
              </Checkbox>
              <Tag style={{ marginLeft: 8 }} color="green">
                始终启用
              </Tag>
              {data.required.desc ? <div className="muted-text">{data.required.desc}</div> : null}
            </div>
          ) : null}
          {data.available.map((service) => (
            <div key={service.id}>
              <Checkbox
                checked={Boolean(service.selected)}
                disabled={save.isPending}
                onChange={(event) => toggle(service.id, event.target.checked)}
              >
                {service.name}
              </Checkbox>
              {service.desc ? <div className="muted-text">{service.desc}</div> : null}
            </div>
          ))}
          {data.available.length === 0 ? <p className="muted-text">管理员还没有开放其他 MCP 服务。</p> : null}
        </div>
      )}
    </>
  )
}
