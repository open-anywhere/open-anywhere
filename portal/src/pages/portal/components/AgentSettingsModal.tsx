import { Alert, App as AntApp, Form, Input, Modal, Select, Spin } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { ApiError, api, type Voice } from '../../../api'

interface Props {
  /** The device whose agent is edited; the dialog is closed when empty. */
  deviceId?: string
  onClose: () => void
}

interface FormValues {
  assistant_name: string
  tts_voice_id?: string
}

function voiceLabel(voice: Voice): string {
  const traits = [voice.gender === 'female' ? '女声' : voice.gender === 'male' ? '男声' : '', ...(voice.styles ?? [])]
    .filter(Boolean)
    .join(' · ')
  return traits ? `${voice.name}（${traits}）` : voice.name
}

export function AgentSettingsModal({ deviceId, onClose }: Props) {
  const [form] = Form.useForm<FormValues>()
  const { message } = AntApp.useApp()
  const queryClient = useQueryClient()
  const open = Boolean(deviceId)

  const agent = useQuery({
    queryKey: ['agent-settings', deviceId],
    queryFn: () => api.getAgentSettings(deviceId as string),
    enabled: open,
    staleTime: 0,
  })
  const voices = useQuery({
    queryKey: ['voices'],
    queryFn: () => api.voices(),
    enabled: open,
    staleTime: 5 * 60_000,
  })

  useEffect(() => {
    if (agent.data) {
      form.setFieldsValue({
        assistant_name: agent.data.agent.assistant_name,
        tts_voice_id: agent.data.agent.tts_voice_id || undefined,
      })
    }
  }, [agent.data, form])

  const save = useMutation({
    mutationFn: (values: FormValues) => {
      const current = agent.data?.agent
      // Send only what changed; the platform keeps everything else.
      const input: { assistant_name?: string; tts_voice_id?: string } = {}
      if (values.assistant_name.trim() !== current?.assistant_name) {
        input.assistant_name = values.assistant_name.trim()
      }
      if (values.tts_voice_id && values.tts_voice_id !== current?.tts_voice_id) {
        input.tts_voice_id = values.tts_voice_id
      }
      return api.updateAgentSettings(deviceId as string, input)
    },
    onSuccess: () => {
      message.success('智能体设置已保存，下一次对话开始生效')
      queryClient.invalidateQueries({ queryKey: ['agent-settings', deviceId] })
      onClose()
    },
    onError: (error) => {
      message.error(error instanceof ApiError ? error.message : '保存失败')
    },
  })

  const submit = async () => {
    const values = await form.validateFields()
    const current = agent.data?.agent
    if (
      values.assistant_name.trim() === current?.assistant_name &&
      (!values.tts_voice_id || values.tts_voice_id === current?.tts_voice_id)
    ) {
      onClose()
      return
    }
    save.mutate(values)
  }

  const selected = voices.data?.voices.find((voice) => voice.id === Form.useWatch('tts_voice_id', form))
  const loadError = agent.error ?? voices.error

  return (
    <Modal
      title="智能体设置"
      open={open}
      okText="保存"
      cancelText="取消"
      confirmLoading={save.isPending}
      okButtonProps={{ disabled: !agent.data }}
      onOk={submit}
      onCancel={onClose}
      destroyOnHidden
    >
      {loadError ? (
        <Alert
          type="error"
          showIcon
          message="暂时无法读取智能体设置"
          description={loadError instanceof ApiError ? loadError.message : String(loadError)}
        />
      ) : agent.isPending || voices.isPending ? (
        <div style={{ padding: 24, textAlign: 'center' }}>
          <Spin />
        </div>
      ) : (
        <Form form={form} layout="vertical" requiredMark={false}>
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
          <Form.Item label="音色" name="tts_voice_id" extra="它说话的声音，可以先试听再选择。">
            <Select
              showSearch
              optionFilterProp="label"
              placeholder="选择音色"
              options={(voices.data?.voices ?? []).map((voice) => ({
                value: voice.id,
                label: voiceLabel(voice),
              }))}
            />
          </Form.Item>
          {selected?.demo ? (
            <audio key={selected.id} src={selected.demo} controls preload="none" style={{ width: '100%' }} />
          ) : null}
          <p className="muted-text" style={{ marginTop: 12 }}>
            如果这个智能体上绑了硬件设备，修改会同时作用在这些设备上。
          </p>
        </Form>
      )}
    </Modal>
  )
}
