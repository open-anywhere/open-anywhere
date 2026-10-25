import {
  AudioOutlined,
  DesktopOutlined,
  PlusOutlined,
  ReloadOutlined,
  RobotOutlined,
} from '@ant-design/icons'
import { Alert, Button, Descriptions, Space, Steps } from 'antd'
import type { Device } from '../../../api'
import { EmptyState } from '../../../components/EmptyState'
import { DeviceStateTag, VoiceTag } from '../../../components/Tags'
import { formatDate, stepLabels } from '../../../lib/format'

const steps = ['创建智能体', '挂载 MCP', '注册设备', '绑定设备', '获取凭证']
const stepOrder = ['create_agent', 'attach_mcp', 'ota', 'bind', 'token', 'done']

function stepIndex(step: string): number {
  const index = stepOrder.indexOf(step)
  return index < 0 ? 0 : Math.min(index, steps.length)
}

interface Props {
  device?: Device
  creating?: boolean
  retrying?: boolean
  onCreate: () => void
  onRetry: () => void
  onCall: () => void
}

export function DevicePanel({ device, creating, retrying, onCreate, onRetry, onCall }: Props) {
  if (!device) {
    return (
      <EmptyState
        icon={<RobotOutlined />}
        title="还没有自己的智能体"
        description="点一次按钮，平台会自动创建灵矽智能体、挂载应用管理 MCP、生成虚拟设备并完成绑定。你不需要接触控制台、API Key 或激活码。"
        action={
          <Button
            type="primary"
            icon={<PlusOutlined />}
            loading={creating}
            onClick={onCreate}
          >
            创建我的智能体
          </Button>
        }
      />
    )
  }

  const current = stepIndex(device.step)

  return (
    <section className="device-panel">
      <div className="device-panel-head">
        <div className="device-identity">
          <span className="device-avatar" aria-hidden="true">
            <RobotOutlined />
          </span>
          <div>
            <div className="device-title-row">
              <h2>{device.agent_name || '你的智能体'}</h2>
              <DeviceStateTag state={device.state} />
              <VoiceTag state={device.voice.state} connected={device.voice.connected} />
            </div>
            <p className="device-meta mono">
              MAC {device.mac}
              {device.agent_id ? ` · Agent ${device.agent_id}` : ''}
            </p>
          </div>
        </div>
        <Space wrap>
          {device.state === 'registered' ? (
            <Button type="primary" icon={<AudioOutlined />} onClick={onCall}>
              发起通话
            </Button>
          ) : null}
          {device.state === 'error' || device.state === 'provisioning' || device.voice.state === 'offline' ? (
            <Button
              icon={<ReloadOutlined />}
              loading={retrying}
              disabled={device.state === 'provisioning'}
              onClick={onRetry}
            >
              {device.state === 'registered' ? '重启语音' : '重试'}
            </Button>
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
            正在自动完成「{stepLabels[device.step] ?? device.step}」，页面会自动刷新。
          </p>
        </div>
      ) : null}

      {device.state === 'error' ? (
        <Alert
          type="error"
          showIcon
          className="panel-alert"
          message={`开通在「${stepLabels[device.step] ?? device.step}」这一步失败`}
          description={device.error || '请稍后重试；平台侧的已有资源会被复用。'}
        />
      ) : null}

      {device.state === 'registered' ? (
        <div className="device-details">
          <Descriptions
            size="small"
            column={{ xs: 1, sm: 2, lg: 3 }}
            items={[
              { key: 'agent', label: '智能体', children: device.agent_name || '—' },
              { key: 'created', label: '创建时间', children: formatDate(device.created_at) },
              { key: 'registered', label: '注册时间', children: formatDate(device.registered_at) },
              { key: 'kind', label: '设备类型', children: device.kind === 'virtual' ? '虚拟设备' : '物理设备' },
              {
                key: 'voice',
                label: '语音运行时',
                children: device.voice.connected ? '在线' : device.voice.state,
              },
            ]}
          />
          {device.voice.error || device.voice.state === 'offline' ? (
            <Alert
              type="warning"
              showIcon
              className="panel-alert"
              message="语音运行时未启动"
              description={
                device.voice.error || '设备已注册，但服务端的语音运行时不在线。可以尝试重新启动。'
              }
              action={
                <Button size="small" loading={retrying} onClick={onRetry}>
                  重启语音
                </Button>
              }
            />
          ) : null}
          <p className="device-note">
            <DesktopOutlined /> 语音运行时由服务端常驻，和真实设备一样保持在线；关闭标签页不会让它下线。
          </p>
        </div>
      ) : null}
    </section>
  )
}
