import { Link } from '@tanstack/react-router'
import { Button, Popconfirm } from 'antd'
import type { Screen } from '../../../api'
import { ScreenStatusTag } from '../../../components/Tags'

export function ScreenRow({
  screen,
  onStop,
  stopping,
}: {
  screen: Screen
  onStop?: () => void
  stopping?: boolean
}) {
  const app = screen.current_app
  return (
    <div className="data-row screen-row">
      <span
        className={`screen-dot ${screen.online ? 'online' : 'offline'}`}
        aria-label={screen.online ? '在线' : '离线'}
      />
      <div className="data-row-main">
        <div className="data-row-heading">
          <strong>{screen.name || screen.screen_id}</strong>
          <ScreenStatusTag status={screen.status} online={screen.online} />
        </div>
        <div className="data-row-meta">
          <span>{screen.online ? '在线' : '离线'}</span>
          {app ? (
            <span>
              正在播放{' '}
              <Link
                to="/portal/apps/$appId"
                params={{ appId: app.app_id }}
                className="text-link"
              >
                {app.name || '应用'}
              </Link>
            </span>
          ) : (
            <span>{screen.status === 'external' ? '正被其他内容占用' : '没有正在播放的内容'}</span>
          )}
        </div>
      </div>
      {app && onStop ? (
        <Popconfirm
          title="停止播放？"
          description={`屏幕上的「${app.name || '这个应用'}」会停下来，之后还可以再让它播放。`}
          okText="停止播放"
          cancelText="取消"
          okButtonProps={{ danger: true, loading: stopping }}
          onConfirm={onStop}
        >
          <Button danger size="small" loading={stopping}>
            停止播放
          </Button>
        </Popconfirm>
      ) : null}
    </div>
  )
}
