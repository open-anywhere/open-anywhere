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
      message.success('已停止播放')
    },
    onError: (error) => {
      message.error(error instanceof ApiError ? error.message : '暂时无法停止播放')
    },
  })

  const list = screens.data?.screens ?? []
  const showNote = !screens.isPending && !screens.error

  return (
    <div className="page">
      <PageHeader
        eyebrow="Screens"
        title="屏幕"
        description="身边的电视与投影，以及它们此刻正在播放的内容。"
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
          message="暂时找不到屏幕"
          description={
            screens.error instanceof ApiError
              ? screens.error.message
              : '投屏服务没有响应，请稍后重试。'
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
          屏幕会自动被发现。想让屏幕播放什么，开口说出来就好。
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
            description="打开电视或投影，确认它和投屏服务连接在同一网络，然后刷新。"
            action={
              <Space wrap>
                <Button
                  icon={<ReloadOutlined />}
                  loading={screens.isFetching}
                  onClick={() => screens.refetch()}
                >
                  再找一次
                </Button>
                <Link to="/portal/services">
                  <Button>检查屏幕连接</Button>
                </Link>
              </Space>
            }
          />
        </section>
      ) : null}
    </div>
  )
}
