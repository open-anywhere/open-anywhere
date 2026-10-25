import {
  AudioMutedOutlined,
  AudioOutlined,
  PauseCircleOutlined,
  PhoneOutlined,
  PlayCircleOutlined,
  SoundOutlined,
  StopOutlined,
} from '@ant-design/icons'
import { Button, Slider, Space } from 'antd'

interface Props {
  supported: boolean
  /** The agent's connection to Linx is down, so a call cannot start. */
  offline?: boolean
  micActive: boolean
  playbackActive: boolean
  starting: boolean
  paused: boolean
  volume: number
  onStart: () => void
  onStop: () => void
  onToggleMicrophone: () => void
  onTogglePlayback: () => void
  onPause: () => void
  onResume: () => void
  onAbort: () => void
  onVolumeChange: (value: number) => void
}

export function VoiceControls({
  supported,
  offline,
  micActive,
  playbackActive,
  starting,
  paused,
  volume,
  onStart,
  onStop,
  onToggleMicrophone,
  onTogglePlayback,
  onPause,
  onResume,
  onAbort,
  onVolumeChange,
}: Props) {
  const active = micActive || playbackActive

  if (!active) {
    return (
      <div className="voice-controls">
        <div className="voice-controls-note">
          {!supported
            ? '当前浏览器不支持语音对话，请使用最新版 Chrome 或 Edge 打开。'
            : offline
              ? '智能体已断开连接，重新连接后才能对话。'
              : '开口就好。浏览器会请求麦克风权限。'}
        </div>
        <Button
          type="primary"
          size="large"
          icon={<PhoneOutlined />}
          loading={starting}
          disabled={!supported || offline}
          onClick={onStart}
        >
          开始对话
        </Button>
      </div>
    )
  }

  return (
    <div className="voice-controls voice-controls-active">
      <Space wrap size={8}>
        <Button
          icon={micActive ? <AudioMutedOutlined /> : <AudioOutlined />}
          onClick={onToggleMicrophone}
        >
          {micActive ? '关闭麦克风' : '开启麦克风'}
        </Button>
        <Button
          icon={playbackActive ? <SoundOutlined /> : <SoundOutlined />}
          onClick={onTogglePlayback}
        >
          {playbackActive ? '关闭播放' : '开启播放'}
        </Button>
        {paused ? (
          <Button icon={<PlayCircleOutlined />} onClick={onResume}>
            继续监听
          </Button>
        ) : (
          <Button icon={<PauseCircleOutlined />} onClick={onPause}>
            暂停监听
          </Button>
        )}
        <Button icon={<StopOutlined />} onClick={onAbort}>
          打断回复
        </Button>
      </Space>
      <div className="voice-volume">
        <SoundOutlined />
        <Slider
          min={0}
          max={100}
          value={volume}
          onChange={onVolumeChange}
          tooltip={{ formatter: (value) => `${value}%` }}
          aria-label="播放音量"
        />
        <span>{volume}%</span>
      </div>
      <Button danger icon={<PhoneOutlined />} onClick={onStop}>
        结束对话
      </Button>
    </div>
  )
}
