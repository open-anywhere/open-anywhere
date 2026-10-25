import {
  ArrowLeftOutlined,
  AudioOutlined,
  PhoneOutlined,
  RobotOutlined,
} from '@ant-design/icons'
import { Alert, Button, Result, Spin } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { Link, useSearch } from '@tanstack/react-router'
import type { Device } from '../../api'
import { ApiError } from '../../api'
import { meQueryOptions } from '../../api/queries'
import { VoiceTag } from '../../components/Tags'
import { useVoiceSession } from '../../hooks/useVoiceSession'
import { usePageTitle } from '../../hooks/usePageTitle'
import { findDevice } from '../../lib/device'
import { TranscriptFeed } from './components/TranscriptFeed'
import { VoiceControls } from './components/VoiceControls'

export function CallPage() {
  usePageTitle('对话')
  const search = useSearch({ from: '/portal/call' })
  const me = useQuery(meQueryOptions)
  const device = findDevice(me.data?.devices, search.device)

  if (me.isPending) {
    return (
      <div className="call-page">
        <div className="call-loading">
          <Spin size="large" />
          <p>正在准备…</p>
        </div>
      </div>
    )
  }

  if (me.error) {
    return (
      <div className="call-page call-result">
        <Result
          status={me.error instanceof ApiError && me.error.status === 401 ? '403' : 'error'}
          title="无法进入通话"
          subTitle={me.error.message}
          extra={
            <Link to="/portal/agents">
              <Button type="primary">返回智能体</Button>
            </Link>
          }
        />
      </div>
    )
  }

  if (!device && search.device) {
    return (
      <div className="call-page call-result">
        <Result
          status="warning"
          title="找不到这个智能体"
          subTitle="它可能已经不存在了。回到智能体页重新开始。"
          extra={
            <Link to="/portal/agents">
              <Button type="primary">返回智能体</Button>
            </Link>
          }
        />
      </div>
    )
  }

  if (!device) {
    return (
      <div className="call-page call-result">
        <Result
          icon={<PhoneOutlined />}
          title="还没有准备好你的智能体"
          subTitle="先准备好它，然后就能直接开口说话。"
          extra={
            <Link to="/portal/agents">
              <Button type="primary">去准备智能体</Button>
            </Link>
          }
        />
      </div>
    )
  }

  if (device.state !== 'registered') {
    return (
      <div className="call-page call-result">
        <Result
          icon={<RobotOutlined />}
          title="智能体还在准备中"
          subTitle="再等一会儿，它就能听见你了。"
          extra={
            <Link to="/portal/agents">
              <Button type="primary">查看准备进度</Button>
            </Link>
          }
        />
      </div>
    )
  }

  return <CallSession device={device} />
}

function CallSession({ device }: { device: Device }) {
  const session = useVoiceSession(device.id)
  const voiceState = session.status?.state ?? device.voice.state
  const connected = session.status?.connected ?? device.voice.connected
  const paused = voiceState === 'idle'

  return (
    <div className="call-page">
      <div className="call-page-inner">
        <header className="call-header">
          <Link to="/portal/agents" className="back-link">
            <ArrowLeftOutlined /> 返回智能体
          </Link>
          <div className="call-identity">
            <span className="call-avatar" aria-hidden="true">
              <RobotOutlined />
            </span>
            <div>
              <h1>{device.agent_name || '你的智能体'}</h1>
              <p className="muted-text">随时可以开口。</p>
            </div>
          </div>
          <div className="call-status">
            <VoiceTag state={voiceState} connected={connected} />
            <span className="muted-text">{connected ? '已连接' : '未连接'}</span>
          </div>
        </header>

        {!session.supported ? (
          <Alert
            type="warning"
            showIcon
            message="当前浏览器不支持语音对话"
            description="请使用最新版 Chrome 或 Edge 打开这个页面。"
          />
        ) : null}

        {session.statusError ? (
          <Alert
            type="warning"
            showIcon
            message="智能体暂时没有响应"
            description={session.statusError}
            action={
              <Link to="/portal/agents">
                <Button size="small">检查智能体</Button>
              </Link>
            }
          />
        ) : null}

        {session.streamError ? (
          <Alert type="info" showIcon message={session.streamError} />
        ) : null}

        {session.error ? <Alert type="error" showIcon message={session.error} /> : null}

        <section className="surface call-transcript-surface">
          <TranscriptFeed entries={session.entries} />
        </section>

        <VoiceControls
          supported={session.supported}
          micActive={session.micActive}
          playbackActive={session.playbackActive}
          starting={session.starting}
          paused={paused}
          volume={session.volume}
          onStart={() => void session.start()}
          onStop={session.stop}
          onToggleMicrophone={() => void session.toggleMicrophone()}
          onTogglePlayback={() => void session.togglePlayback()}
          onPause={() => void session.pause()}
          onResume={() => void session.resume()}
          onAbort={() => void session.abort()}
          onVolumeChange={session.setVolume}
        />

        <p className="call-hint">
          <AudioOutlined /> 浏览器会请求麦克风权限。结束对话只会停止这一页的声音。
        </p>
      </div>
    </div>
  )
}
