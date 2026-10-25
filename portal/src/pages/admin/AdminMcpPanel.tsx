import { SyncOutlined } from '@ant-design/icons'
import { Alert, App as AntApp, Avatar, Button, Checkbox, List, Skeleton, Tag } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ApiError, adminApi, type McpService } from '../../api'

const queryKey = ['admin', 'mcp'] as const

/** Lists the MCP services on the Linx account and chooses which ones users may add. */
export function AdminMcpPanel() {
  const { message } = AntApp.useApp()
  const queryClient = useQueryClient()
  const services = useQuery({ queryKey, queryFn: adminApi.mcp, retry: false })

  const share = useMutation({
    mutationFn: (ids: string[]) => adminApi.setSharedMcp(ids),
    onSuccess: () => queryClient.invalidateQueries({ queryKey }),
    onError: (error) => message.error(error instanceof ApiError ? error.message : '保存失败'),
  })
  const sync = useMutation({
    mutationFn: adminApi.syncMcp,
    onSuccess: () => message.success('已把当前地址和 Token 同步到灵矽的 MCP 服务'),
    onError: (error) => message.error(error instanceof ApiError ? error.message : '同步失败'),
  })

  const list = services.data?.services ?? []
  const toggle = (service: McpService, checked: boolean) => {
    const current = list.filter((item) => item.shared).map((item) => item.id)
    share.mutate(checked ? [...current, service.id] : current.filter((id) => id !== service.id))
  }

  return (
    <section className="surface settings-section">
      <div className="section-header">
        <div>
          <h2>MCP 服务</h2>
          <p>
            灵矽账号下已有的 MCP 服务。网关自己的服务会自动加到每个智能体上，用户不能移除。勾选“开放给用户”的服务，用户可以在自己智能体的设置里添加。
          </p>
        </div>
        <Button icon={<SyncOutlined />} loading={sync.isPending} onClick={() => sync.mutate()}>
          同步地址与 Token
        </Button>
      </div>
      {services.isPending ? (
        <Skeleton active paragraph={{ rows: 3 }} />
      ) : services.isError ? (
        <Alert
          type="warning"
          showIcon
          message="暂时无法读取灵矽的 MCP 服务"
          description={
            services.error instanceof ApiError
              ? services.error.message
              : '请先在“灵矽平台”里配置并保存 API Key。'
          }
        />
      ) : (
        <>
          {!services.data.provisioned ? (
            <Alert
              type="info"
              showIcon
              style={{ marginBottom: 12 }}
              message="网关自己的 MCP 服务还没有创建"
              description="第一个用户的智能体开通时会自动创建，之后这里会显示它。"
            />
          ) : null}
          <List
            dataSource={list}
            locale={{ emptyText: '灵矽账号下还没有 MCP 服务' }}
            renderItem={(service) => (
              <List.Item
                key={service.id}
                actions={[
                  service.required ? (
                    <Tag key="required" color="green">
                      始终启用
                    </Tag>
                  ) : (
                    <Checkbox
                      key="share"
                      checked={service.shared}
                      disabled={share.isPending}
                      onChange={(event) => toggle(service, event.target.checked)}
                    >
                      开放给用户
                    </Checkbox>
                  ),
                ]}
              >
                <List.Item.Meta
                  avatar={service.logo_url ? <Avatar src={service.logo_url} shape="square" /> : null}
                  title={
                    <span>
                      {service.name}
                      {service.required ? <Tag style={{ marginLeft: 8 }}>网关自己的服务</Tag> : null}
                      {service.private ? <Tag style={{ marginLeft: 8 }}>私有</Tag> : null}
                    </span>
                  }
                  description={service.desc || '—'}
                />
              </List.Item>
            )}
          />
        </>
      )}
    </section>
  )
}
