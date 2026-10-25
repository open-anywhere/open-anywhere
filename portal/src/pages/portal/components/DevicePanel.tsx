import {
  AudioOutlined,
  DeleteOutlined,
  DesktopOutlined,
  DisconnectOutlined,
  PlusOutlined,
  ReloadOutlined,
  RobotOutlined,
} from '@ant-design/icons'
import { Alert, Button, Descriptions, Popconfirm, Space, Steps, Tag } from 'antd'
import type { Device } from '../../../api'
import { EmptyState } from '../../../components/EmptyState'
import { DeviceStateTag, VoiceTag } from '../../../components/Tags'
import { formatDate } from '../../../lib/format'

const steps = ['准备智能体', '接入屏幕', '可以开始']

function stepIndex(step: string): number {
  switch (step) {
    case 'create_agent':
    case 'attach_mcp':
      return 0
    case 'ota':
    case 'bind':
      return 1
    case 'token':
    case 'done':
      return 2
    default:
      return 0
  }
}

interface Props {
  device?: Device
  creating?: boolean
  retrying?: boolean
  disconnecting?: boolean
  reconnecting?: boolean
  onCreate: () => void
  onRetry: () => void
  onCall: () => void
  onDisconnect?: () => void
  onReconnect?: () => void
  /** Present only when the device may be removed. */
  onRemove?: () => void
  removing?: boolean
}

export function DevicePanel({
  device,
  creating,
  retrying,
  disconnecting,
  reconnecting,
  onCreate,
  onRetry,
  onCall,
  onDisconnect,
  onReconnect,
  onRemove,
  removing,
}: Props) {
  if (!device) {
    return (
      <EmptyState
        icon={<RobotOutlined />}
        title="还没有自己的智能体"
        description="准备好之后，你就能开口描述想做的事，让应用出现在身边的屏幕上。"
        action={
          <Button
            type="primary"
            icon={<PlusOutlined />}
            loading={creating}
            onClick={onCreate}
          >
            准备好我的智能体
          </Button>
        }
      />
    )
  }

  const current = stepIndex(device.step)
  // Hardware keeps its own connection to Linx; only virtual devices have a
  // browser voice session to talk to, connect or disconnect.
  const hardware = device.kind === 'physical'
  const voiceOffline = !hardware && device.voice.state === 'offline'
  const title = device.name || device.agent_name || (hardware ? '硬件设备' : '你的智能体')

  return (
    <section className="device-panel">
      <div className="device-panel-head">
        <div className="device-identity">
          <span className="device-avatar" aria-hidden="true">
            <RobotOutlined />
          </span>
          <div>
            <div className="device-title-row">
              <h2>{title}</h2>
              <DeviceStateTag state={device.state} />
              {hardware ? (
                <Tag>硬件设备</Tag>
              ) : (
                <VoiceTag state={device.voice.state} connected={device.voice.connected} />
              )}
            </div>
            <p className="device-meta">
              {hardware
                ? '设备自己连接灵矽，对它说话就能让应用出现在屏幕上。'
                : '随时可以开口，让它为你准备一个应用。'}
            </p>
          </div>
        </div>
        <Space wrap>
          {device.state === 'registered' && !hardware ? (
            <Button type="primary" icon={<AudioOutlined />} disabled={voiceOffline} onClick={onCall}>
              开始对话
            </Button>
          ) : null}
          {device.state === 'registered' && !hardware && onReconnect ? (
            <Button icon={<ReloadOutlined />} loading={reconnecting} onClick={onReconnect}>
              重新连接
            </Button>
          ) : null}
          {device.state === 'registered' && !hardware && onDisconnect && !voiceOffline ? (
            <Popconfirm
              title="断开与灵矽的连接？"
              description="正在进行的对话会中断，之后可以随时重新连接。"
              okText="断开"
              cancelText="取消"
              onConfirm={onDisconnect}
            >
              <Button icon={<DisconnectOutlined />} loading={disconnecting}>
                断开连接
              </Button>
            </Popconfirm>
          ) : null}
          {device.state === 'error' ||
          device.state === 'provisioning' ||
          (device.state === 'registered' && !onReconnect && voiceOffline) ? (
            <Button
              icon={<ReloadOutlined />}
              loading={retrying}
              disabled={device.state === 'provisioning'}
              onClick={onRetry}
            >
              {device.state === 'registered' ? '重新连接' : '重试'}
            </Button>
          ) : null}
          {onRemove ? (
            <Popconfirm
              title={hardware ? '移除这台硬件设备？' : '移除这个智能体设备？'}
              description={
                hardware
                  ? '会在灵矽上解绑它，之后可以用新的激活码重新添加。'
                  : '浏览器里的语音连接会关闭。它名下还有应用时需要先删除应用。'
              }
              okText="移除"
              cancelText="取消"
              okButtonProps={{ danger: true }}
              onConfirm={onRemove}
            >
              <Button danger icon={<DeleteOutlined />} loading={removing}>
                移除
              </Button>
            </Popconfirm>
          ) : null}
        </Space>
      </div>

      {device.state === 'provisioning' ? (
        <div className="device-steps">
          <Steps
            size="small"
            current={current}
            responsive
            items={steps.map((title) => ({ title }))}
          />
          <p className="muted-text">
            正在准备你的智能体，通常只需要一会儿。页面会自动刷新。
          </p>
        </div>
      ) : null}

      {device.state === 'error' ? (
        <Alert
          type="error"
          showIcon
          className="panel-alert"
          message="准备过程中遇到了问题"
          description={device.error || '请稍后重试。'}
        />
      ) : null}

      {device.state === 'registered' ? (
        <div className="device-details">
          <Descriptions
            size="small"
            column={{ xs: 1, sm: 2, lg: 3 }}
            items={[
              { key: 'agent', label: '智能体', children: device.agent_name || '—' },
              ...(hardware ? [{ key: 'mac', label: '设备 ID', children: device.mac }] : []),
              { key: 'created', label: '创建时间', children: formatDate(device.created_at) },
              { key: 'registered', label: '准备好于', children: formatDate(device.registered_at) },
            ]}
          />
          {!hardware && (device.voice.error || voiceOffline) ? (
            <Alert
              type="warning"
              showIcon
              className="panel-alert"
              message="智能体暂时不在线"
              description={
                device.voice.error || '连接已断开或没有响应，可以重新连接。'
              }
              action={
                <Button
                  size="small"
                  loading={onReconnect ? reconnecting : retrying}
                  onClick={onReconnect ?? onRetry}
                >
                  重新连接
                </Button>
              }
            />
          ) : null}
          <p className="device-note">
            <DesktopOutlined /> 关掉这个页面也没关系，智能体还会在。
          </p>
        </div>
      ) : null}
    </section>
  )
}
