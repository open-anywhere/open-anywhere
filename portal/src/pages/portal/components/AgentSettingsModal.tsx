import {
  Alert,
  App as AntApp,
  Button,
  Divider,
  Drawer,
  Form,
  Input,
  InputNumber,
  Select,
  Slider,
  Space,
  Spin,
  Switch,
} from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { ApiError, api, type AgentSettings, type Voice } from '../../../api'
import { useDeviceConnection } from '../../../hooks/useDeviceConnection'
import { AgentMcpGroup } from './AgentMcpGroup'

interface Props {
  /** The device whose agent is edited; the dialog is closed when empty. */
  deviceId?: string
  /** A browser device can be reconnected from here; hardware has to be restarted. */
  reconnectable?: boolean
  onClose: () => void
}

// Greeting is shown as "greet after wake-up", the inverse of the platform flag.
type FormValues = Omit<AgentSettings, 'disable_greeting'> & { greet?: boolean }

// What the console shows when the platform reports no value. They are only
// displayed; a field the user did not touch is never sent.
const defaults = {
  voice_volume: 50,
  voice_speed: 1,
  llm_temperature: 0.7,
  llm_max_tokens: 500,
  idle_timeout_seconds: 120,
}

const optionalNumbers = [
  'voice_volume',
  'voice_speed',
  'llm_temperature',
  'llm_max_tokens',
  'idle_timeout_seconds',
] as const

function voiceLabel(voice: Voice): string {
  // Style tags come from the platform as codes such as "wenrou"; only the
  // readable ones are worth showing.
  const readable = (voice.styles ?? []).filter((style) => /[\u4e00-\u9fff]/.test(style))
  const traits = [voice.gender === 'female' ? '女声' : voice.gender === 'male' ? '男声' : '', ...readable]
    .filter(Boolean)
    .join(' · ')
  const label = traits ? `${voice.name}（${traits}）` : voice.name
  // Pro voices cost more to run than free ones.
  return voice.tier === 'pro' ? `${label} · 高级` : label
}

export function AgentSettingsModal({ deviceId, reconnectable, onClose }: Props) {
  const [form] = Form.useForm<FormValues>()
  const { message, notification } = AntApp.useApp()
  const { reconnect } = useDeviceConnection()
  const queryClient = useQueryClient()
  const open = Boolean(deviceId)

  const agent = useQuery({
    queryKey: ['agent-settings', deviceId],
    queryFn: () => api.getAgentSettings(deviceId as string),
    enabled: open,
    staleTime: 0,
  })
  const options = useQuery({
    queryKey: ['agent-options'],
    queryFn: () => api.agentOptions(),
    enabled: open,
    staleTime: 5 * 60_000,
  })

  useEffect(() => {
    if (agent.data) {
      const current = agent.data.agent
      form.setFieldsValue({
        assistant_name: current.assistant_name,
        tts_voice_id: current.tts_voice_id || undefined,
        lang_code: current.lang_code || undefined,
        llm_model_id: current.llm_model_id || undefined,
        mem_model_id: current.mem_model_id || undefined,
        voice_volume: current.voice_volume ?? defaults.voice_volume,
        voice_speed: current.voice_speed ?? defaults.voice_speed,
        llm_temperature: current.llm_temperature ?? defaults.llm_temperature,
        llm_max_tokens: current.llm_max_tokens ?? defaults.llm_max_tokens,
        idle_timeout_seconds: current.idle_timeout_seconds ?? defaults.idle_timeout_seconds,
        greet: !current.disable_greeting,
        system_prompt: current.system_prompt ?? '',
        chat_history: current.chat_history,
      })
    }
  }, [agent.data, form])

  // Collect only what the user changed; the platform keeps everything else.
  const changes = (values: FormValues): Partial<AgentSettings> => {
    const current = agent.data?.agent
    const input: Partial<AgentSettings> = {}
    const name = values.assistant_name.trim()
    if (name !== current?.assistant_name) input.assistant_name = name
    const prompt = (values.system_prompt ?? '').trim()
    if (prompt !== (current?.system_prompt ?? '').trim()) input.system_prompt = prompt
    if (values.tts_voice_id && values.tts_voice_id !== current?.tts_voice_id) input.tts_voice_id = values.tts_voice_id
    if (values.lang_code && values.lang_code !== current?.lang_code) input.lang_code = values.lang_code
    if (values.llm_model_id && values.llm_model_id !== current?.llm_model_id) input.llm_model_id = values.llm_model_id
    if (values.mem_model_id && values.mem_model_id !== current?.mem_model_id) input.mem_model_id = values.mem_model_id
    if (values.chat_history !== undefined && values.chat_history !== current?.chat_history) {
      input.chat_history = values.chat_history
    }
    for (const key of optionalNumbers) {
      const value = values[key]
      if (value === undefined || value === null) continue
      // With no platform value there is nothing to compare against, so a
      // field counts as changed once the user has touched it.
      const changed = current?.[key] === undefined ? form.isFieldTouched(key) : value !== current[key]
      if (changed) input[key] = value
    }
    if (values.greet !== undefined && values.greet !== !current?.disable_greeting) {
      input.disable_greeting = !values.greet
    }
    return input
  }

  const save = useMutation({
    mutationFn: (input: Partial<AgentSettings>) => api.updateAgentSettings(deviceId as string, input),
    onSuccess: () => {
      if (reconnectable) {
        const id = deviceId as string
        const key = `reconnect-${id}`
        notification.success({
          key,
          message: '智能体设置已保存',
          description: '重新连接后，新设置才会生效。',
          duration: 12,
          btn: (
            <Button
              type="primary"
              size="small"
              onClick={() => {
                reconnect.mutate(id)
                notification.destroy(key)
              }}
            >
              现在重新连接
            </Button>
          ),
        })
      } else {
        message.success('智能体设置已保存。硬件设备需要重启，新设置才会生效')
      }
      queryClient.invalidateQueries({ queryKey: ['agent-settings', deviceId] })
      onClose()
    },
    onError: (error) => {
      message.error(error instanceof ApiError ? error.message : '保存失败')
    },
  })

  const submit = async () => {
    // A rejected validation is shown on the form; it must not surface as an
    // unhandled rejection.
    const values = await form.validateFields().catch(() => undefined)
    if (!values) return
    const input = changes(values)
    if (Object.keys(input).length === 0) {
      onClose()
      return
    }
    save.mutate(input)
  }

  const selectedVoiceId = Form.useWatch('tts_voice_id', form)
  const selectedLang = Form.useWatch('lang_code', form)
  const selected = options.data?.voices.find((voice) => voice.id === selectedVoiceId)
  const loadError = agent.error ?? options.error
  const toSelect = (list: { id: string; name: string; comment?: string }[] | undefined) =>
    (list ?? []).map((item) => ({
      value: item.id,
      label: item.comment ? `${item.name}（${item.comment}）` : item.name,
    }))
  // A mode the agent already carries but the portal does not offer stays
  // visible instead of showing a bare id.
  const currentMem = agent.data?.agent.mem_model_id
  const memoryOptions = toSelect(options.data?.memory_modes)
  if (currentMem && !memoryOptions.some((option) => option.value === currentMem)) {
    memoryOptions.unshift({ value: currentMem, label: `${currentMem}（当前）` })
  }

  return (
    <Drawer
      title="智能体设置"
      open={open}
      size="large"
      onClose={onClose}
      destroyOnHidden
      footer={
        <Space style={{ display: 'flex', justifyContent: 'flex-end' }}>
          <Button onClick={onClose}>取消</Button>
          <Button type="primary" loading={save.isPending} disabled={!agent.data} onClick={submit}>
            保存
          </Button>
        </Space>
      }
    >
      {loadError ? (
        <Alert
          type="error"
          showIcon
          message="暂时无法读取智能体设置"
          description={loadError instanceof ApiError ? loadError.message : String(loadError)}
        />
      ) : agent.isPending || options.isPending ? (
        <div style={{ padding: 24, textAlign: 'center' }}>
          <Spin />
        </div>
      ) : (
        <Form form={form} layout="vertical" requiredMark={false}>
          <Divider titlePlacement="start" plain style={{ marginTop: 0 }}>
            身份与语言
          </Divider>
          <Form.Item
            label="助手名称"
            name="assistant_name"
            extra="智能体对自己的称呼。"
            rules={[
              { required: true, whitespace: true, message: '请填写助手名称' },
              { max: 20, message: '最长 20 个字符' },
            ]}
          >
            <Input autoComplete="off" />
          </Form.Item>
          <Form.Item label="对话语言" name="lang_code" extra="智能体听和说使用的语言。">
            <Select placeholder="选择语言" options={toSelect(options.data?.languages)} />
          </Form.Item>

          <Divider titlePlacement="start" plain>
            角色介绍
          </Divider>
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 12 }}
            message="这是智能体的完整角色设定"
            description="平台默认的角色介绍写了智能体怎样制作应用、怎样用语音控制屏幕上的应用。改动它可能让智能体无法再正常制作或控制应用。改乱了可以点“恢复平台默认”。"
          />
          <Form.Item
            name="system_prompt"
            rules={[
              { required: true, whitespace: true, message: '角色介绍不能为空' },
              { max: 6000, message: '最长 6000 个字符' },
            ]}
          >
            <Input.TextArea rows={12} maxLength={6000} showCount />
          </Form.Item>
          <Button
            style={{ marginBottom: 16 }}
            disabled={!options.data?.default_system_prompt}
            onClick={() => form.setFieldValue('system_prompt', options.data?.default_system_prompt ?? '')}
          >
            恢复平台默认
          </Button>

          <Divider titlePlacement="start" plain>
            声音
          </Divider>
          <Form.Item label="音色" name="tts_voice_id" extra="它说话的声音，列表按所选语言过滤，可以先试听再选择。带“高级”的音色运行成本更高。">
            <Select
              showSearch
              optionFilterProp="label"
              placeholder="选择音色"
              options={(options.data?.voices ?? [])
                // Offer the voices that speak the chosen language, but never
                // hide the one the agent already uses.
                .filter(
                  (voice) =>
                    !selectedLang ||
                    !voice.languages?.length ||
                    voice.languages.includes(selectedLang) ||
                    voice.id === agent.data?.agent.tts_voice_id,
                )
                .map((voice) => ({
                  value: voice.id,
                  label: voiceLabel(voice),
                }))}
            />
          </Form.Item>
          {selected?.demo ? (
            <audio
              key={selected.id}
              src={selected.demo}
              controls
              preload="none"
              style={{ width: '100%', marginBottom: 16 }}
            />
          ) : null}
          <Form.Item label="语速" name="voice_speed" extra="1.0 为正常速度。试听播放的是示例音频，调整后的效果要保存后用新对话验证。">
            <Slider min={0.5} max={2} step={0.1} />
          </Form.Item>
          <Form.Item label="音量" name="voice_volume" extra="最终音量还受设备和系统音量影响。">
            <Slider min={0} max={100} step={5} />
          </Form.Item>

          <Divider titlePlacement="start" plain>
            回答方式
          </Divider>
          <Form.Item label="大模型" name="llm_model_id" extra="决定理解和回答的能力、速度与风格。">
            <Select showSearch optionFilterProp="label" placeholder="选择大模型" options={toSelect(options.data?.llm_models)} />
          </Form.Item>
          <Form.Item
            label="回答随机性"
            name="llm_temperature"
            extra="调低回答更稳定，调高更有变化。范围 0 到 1.9。"
          >
            <Slider min={0} max={1.9} step={0.1} />
          </Form.Item>
          <Form.Item
            label="单次回复长度上限"
            name="llm_max_tokens"
            extra="限制一次回答的长度，过小会让回答被截断。范围 1 到 8192。"
          >
            <InputNumber min={1} max={8192} step={50} style={{ width: 160 }} />
          </Form.Item>

          <Divider titlePlacement="start" plain>
            记忆与隐私
          </Divider>
          <Form.Item label="记忆" name="mem_model_id" extra="长期记忆会从交流中提取偏好，供之后的对话参考。">
            <Select options={memoryOptions} />
          </Form.Item>
          <Form.Item
            label="聊天保存"
            name="chat_history"
            extra="决定平台保存哪些聊天记录。它和记忆是两项独立的设置，选择“不记录”不会删除已经保存的内容，也不会关闭长期记忆。"
          >
            <Select
              options={[
                { value: 0, label: '不记录' },
                { value: 1, label: '仅文本' },
                { value: 2, label: '文本 + 语音' },
              ]}
            />
          </Form.Item>

          <Divider titlePlacement="start" plain>
            对话
          </Divider>
          <Form.Item
            label="空闲断开时间（秒）"
            name="idle_timeout_seconds"
            extra="安静多久后结束对话。范围 60 到 3600。"
          >
            <InputNumber min={60} max={3600} step={30} style={{ width: 160 }} />
          </Form.Item>
          <Form.Item label="唤醒后问候" name="greet" valuePropName="checked" extra="听到唤醒词后先打招呼。">
            <Switch />
          </Form.Item>

          <AgentMcpGroup deviceId={deviceId as string} />

          <p className="muted-text" style={{ marginTop: 12 }}>
            如果这个智能体上绑了硬件设备，修改会同时作用在这些设备上。意图识别由平台管理，这里不能修改。
          </p>
        </Form>
      )}
    </Drawer>
  )
}
