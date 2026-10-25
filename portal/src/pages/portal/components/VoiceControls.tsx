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
          {supported
            ? '通话使用浏览器 WebCodecs 编解码 Opus 音频，需要麦克风权限。'
            : '当前浏览器不支持 WebCodecs 音频，请使用 Chrome 或 Edge，并通过 localhost 或 HTTPS 访问。'}
        </div>
        <Button
          type="primary"
          size="large"
          icon={<PhoneOutlined />}
          loading={starting}
          disabled={!supported}
          onClick={onStart}
        >
          开始通话
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
        结束通话
      </Button>
    </div>
  )
}
