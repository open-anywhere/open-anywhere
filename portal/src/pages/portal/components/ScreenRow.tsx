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
              正在展示{' '}
              <Link
                to="/portal/apps/$appId"
                params={{ appId: app.app_id }}
                className="text-link"
              >
                {app.name || '应用'}
              </Link>
            </span>
          ) : (
            <span>{screen.status === 'external' ? '被其他投屏占用' : '没有正在展示的应用'}</span>
          )}
        </div>
      </div>
      {app && onStop ? (
        <Popconfirm
          title="停止投屏？"
          description={`将停止「${app.name || '这个应用'}」并释放屏幕。之后可以通过语音重新开始。`}
          okText="停止并释放"
          cancelText="取消"
          okButtonProps={{ danger: true, loading: stopping }}
          onConfirm={onStop}
        >
          <Button danger size="small" loading={stopping}>
            停止投屏
          </Button>
        </Popconfirm>
      ) : null}
    </div>
  )
}
