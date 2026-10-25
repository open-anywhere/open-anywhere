import { DesktopOutlined, ReloadOutlined } from '@ant-design/icons'
import { Alert, App as AntApp, Button, Skeleton, Space } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ApiError, api } from '../../api'
import { screensQueryOptions } from '../../api/queries'
import { EmptyState } from '../../components/EmptyState'
import { usePageTitle } from '../../hooks/usePageTitle'
import { PageHeader } from './components/PageHeader'
import { ScreenRow } from './components/ScreenRow'

export function ScreensPage() {
  usePageTitle('屏幕')
  const queryClient = useQueryClient()
  const { message } = AntApp.useApp()
  const screens = useQuery(screensQueryOptions)

  const stop = useMutation({
    mutationFn: (screenId: string) => api.stopScreen(screenId),
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: ['screens'] })
      queryClient.invalidateQueries({ queryKey: ['apps'] })
      queryClient.invalidateQueries({ queryKey: ['app', result.app.app_id] })
      message.success('已停止投屏并释放屏幕')
    },
    onError: (error) => {
      message.error(error instanceof ApiError ? error.message : '停止投屏失败')
    },
  })

  const list = screens.data?.screens ?? []
  const showNote = !screens.isPending && !screens.error

  return (
    <div className="page">
      <PageHeader
        eyebrow="Screens"
        title="屏幕"
        description="投屏服务发现的显示设备，以及它们此刻展示的内容。"
        actions={
          <Space>
            <Button
              icon={<ReloadOutlined />}
              loading={screens.isFetching}
              onClick={() => screens.refetch()}
            >
              刷新
            </Button>
          </Space>
        }
      />

      {screens.error ? (
        <Alert
          type="error"
          showIcon
          className="panel-alert"
          message="无法发现屏幕"
          description={
            screens.error instanceof ApiError
              ? screens.error.message
              : '投屏服务暂时不可用，请稍后重试。'
          }
          action={
            <Button size="small" loading={screens.isFetching} onClick={() => screens.refetch()}>
              重试
            </Button>
          }
        />
      ) : null}

      {showNote ? (
        <div className="hint-note">
          屏幕由你在服务页绑定的投屏服务自动发现；投屏动作通过语音触发。停止投屏会停止屏幕上的应用并释放屏幕，之后可以通过语音重新开始。
        </div>
      ) : null}

      {screens.isPending ? (
        <section className="surface">
          <Skeleton active paragraph={{ rows: 5 }} title={false} />
        </section>
      ) : list.length ? (
        <section className="surface surface-flush">
          <div className="data-list">
            {list.map((screen) => (
              <ScreenRow
                key={screen.screen_id}
                screen={screen}
                stopping={stop.isPending && stop.variables === screen.screen_id}
                onStop={
                  screen.current_app
                    ? () => stop.mutate(screen.screen_id)
                    : undefined
                }
              />
            ))}
          </div>
        </section>
      ) : !screens.error ? (
        <section className="surface">
          <EmptyState
            icon={<DesktopOutlined />}
            title="还没有发现屏幕"
            description="确保电视、投影或其他显示设备已接入你绑定的投屏服务，然后刷新这个页面。屏幕由投屏服务发现，Portal 不直接扫描局域网。"
            action={
              <Space wrap>
                <Button
                  icon={<ReloadOutlined />}
                  loading={screens.isFetching}
                  onClick={() => screens.refetch()}
                >
                  重新发现
                </Button>
                <Link to="/portal/services">
                  <Button>检查服务绑定</Button>
                </Link>
              </Space>
            }
          />
        </section>
      ) : null}
    </div>
  )
}
