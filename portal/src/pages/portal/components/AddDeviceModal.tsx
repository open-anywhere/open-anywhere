import { Form, Input, Modal, Radio, Select } from 'antd'
import { useEffect } from 'react'
import type { CreateDeviceInput, Device } from '../../../api'

interface FormValues {
  kind: 'physical' | 'virtual'
  name?: string
  activation_code?: string
  agent_device_id?: string
}

interface Props {
  open: boolean
  devices: Device[]
  submitting: boolean
  onCancel: () => void
  onSubmit: (input: CreateDeviceInput) => void
}

export function AddDeviceModal({ open, devices, submitting, onCancel, onSubmit }: Props) {
  const [form] = Form.useForm<FormValues>()
  const kind = Form.useWatch('kind', form)
  // Hardware joins one of the agents the account already has.
  const agents = devices.filter((device) => device.agent_id)

  useEffect(() => {
    if (!open) return
    form.resetFields()
    form.setFieldsValue({
      kind: agents.length ? 'physical' : 'virtual',
      agent_device_id: agents[0]?.id,
    })
    // Reset when the dialog opens, not whenever the device list refreshes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, form])

  const submit = async () => {
    const values = await form.validateFields()
    const input: CreateDeviceInput = { kind: values.kind, name: values.name?.trim() || undefined }
    if (values.kind === 'physical') {
      input.activation_code = values.activation_code?.trim()
      input.agent_device_id = values.agent_device_id
    }
    onSubmit(input)
  }

  return (
    <Modal
      title="添加设备"
      open={open}
      okText={kind === 'physical' ? '绑定设备' : '创建'}
      cancelText="取消"
      confirmLoading={submitting}
      onOk={submit}
      onCancel={onCancel}
      destroyOnHidden
    >
      <Form form={form} layout="vertical" requiredMark={false}>
        <Form.Item label="设备类型" name="kind">
          <Radio.Group>
            <Radio.Button value="physical" disabled={!agents.length}>
              硬件设备
            </Radio.Button>
            <Radio.Button value="virtual">虚拟设备</Radio.Button>
          </Radio.Group>
        </Form.Item>
        <Form.Item
          label="名称"
          name="name"
          extra="可选，方便区分，例如“客厅”。"
          rules={[{ max: 40, message: '最长 40 个字符' }]}
        >
          <Input autoComplete="off" placeholder="客厅" />
        </Form.Item>
        {kind === 'physical' ? (
          <>
            <Form.Item
              label="设备激活码"
              name="activation_code"
              extra="给硬件接上电源并联网，激活码会由设备播报或显示在屏幕上，把它填在这里。每个激活码只能使用一次。"
              rules={[
                { required: true, message: '请填写设备激活码' },
                { pattern: /^[A-Za-z0-9]{1,32}$/, message: '激活码只包含字母和数字' },
              ]}
            >
              <Input autoComplete="off" placeholder="例如 123456" />
            </Form.Item>
            {agents.length > 1 ? (
              <Form.Item
                label="加入哪个智能体"
                name="agent_device_id"
                extra="硬件会使用这个智能体的人设，它做的应用也归在这个智能体下。"
              >
                <Select
                  options={agents.map((device) => ({
                    value: device.id,
                    label: device.name || device.agent_name || device.mac,
                  }))}
                />
              </Form.Item>
            ) : null}
          </>
        ) : (
          <p className="muted-text">
            虚拟设备会创建一个新的智能体，在浏览器里用麦克风和扬声器对话，不需要硬件。
          </p>
        )}
      </Form>
    </Modal>
  )
}
