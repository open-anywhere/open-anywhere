import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { AddDeviceModal } from './AddDeviceModal'
import { makeDevice } from '../../../test/fixtures'
import { renderInRouter } from '../../../test/router'

async function setup(overrides: Partial<Parameters<typeof AddDeviceModal>[0]> = {}) {
  const onSubmit = vi.fn()
  const onCancel = vi.fn()
  await renderInRouter(
    <AddDeviceModal
      open
      devices={[]}
      submitting={false}
      onCancel={onCancel}
      onSubmit={onSubmit}
      {...overrides}
    />,
  )
  return { onSubmit, onCancel }
}

/** The physical form appears after the dialog's first paint. */
async function hardwareForm() {
  return screen.findByRole('button', { name: '绑定设备' })
}

describe('AddDeviceModal', () => {
  it('creates a virtual device by default when the account has no agent', async () => {
    const user = userEvent.setup()
    const { onSubmit } = await setup()

    expect(screen.getByText('添加设备')).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: '硬件设备' })).toBeDisabled()
    expect(
      screen.getByText('虚拟设备会创建一个新的智能体，在浏览器里用麦克风和扬声器对话，不需要硬件。'),
    ).toBeInTheDocument()

    await user.click(await screen.findByRole('button', { name: /创\s*建/ }))
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith({ kind: 'virtual', name: undefined }))
  })

  it('trims the optional name', async () => {
    const user = userEvent.setup()
    const { onSubmit } = await setup()

    await user.type(await screen.findByPlaceholderText('客厅'), '  卧室  ')
    await user.click(await screen.findByRole('button', { name: /创\s*建/ }))
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith({ kind: 'virtual', name: '卧室' }))
  })

  it('rejects a name longer than 40 characters', async () => {
    const user = userEvent.setup()
    const { onSubmit } = await setup()

    await user.type(await screen.findByPlaceholderText('客厅'), 'a'.repeat(41))
    await user.click(await screen.findByRole('button', { name: /创\s*建/ }))

    expect(await screen.findByText('最长 40 个字符')).toBeInTheDocument()
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('cancels without submitting', async () => {
    const user = userEvent.setup()
    const { onCancel, onSubmit } = await setup()

    await user.click(screen.getByRole('button', { name: /取\s*消/ }))
    expect(onCancel).toHaveBeenCalledTimes(1)
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('shows the submit button busy while creating', async () => {
    await setup({ submitting: true })
    expect(await screen.findByRole('button', { name: /创\s*建/ })).toHaveClass('ant-btn-loading')
  })

  it('binds hardware to the existing agent by default', async () => {
    const user = userEvent.setup()
    const agent = makeDevice({ id: 'dev_agent', name: '主智能体' })
    const { onSubmit } = await setup({ devices: [agent] })

    const ok = await hardwareForm()
    expect(screen.getByText('设备激活码')).toBeInTheDocument()
    // A single agent needs no choice.
    expect(screen.queryByText('加入哪个智能体')).toBeNull()

    await user.type(screen.getByPlaceholderText('例如 123456'), 'ABC123')
    await user.click(ok)

    await waitFor(() =>
      expect(onSubmit).toHaveBeenCalledWith({
        kind: 'physical',
        name: undefined,
        activation_code: 'ABC123',
        agent_device_id: 'dev_agent',
      }),
    )
  })

  it('requires an activation code for hardware', async () => {
    const user = userEvent.setup()
    const { onSubmit } = await setup({ devices: [makeDevice()] })

    await user.click(await hardwareForm())
    expect(await screen.findByText('请填写设备激活码')).toBeInTheDocument()
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('accepts only letters and digits in an activation code', async () => {
    const user = userEvent.setup()
    const { onSubmit } = await setup({ devices: [makeDevice()] })

    const ok = await hardwareForm()
    await user.type(screen.getByPlaceholderText('例如 123456'), '12-34')
    await user.click(ok)

    expect(await screen.findByText('激活码只包含字母和数字')).toBeInTheDocument()
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('lets the user choose between several agents', async () => {
    const user = userEvent.setup()
    const first = makeDevice({ id: 'dev_a', name: '客厅助手' })
    const second = makeDevice({ id: 'dev_b', name: '书房助手' })
    const { onSubmit } = await setup({ devices: [first, second] })

    const ok = await hardwareForm()
    expect(screen.getByText('加入哪个智能体')).toBeInTheDocument()

    await user.type(screen.getByPlaceholderText('例如 123456'), 'ABC')
    await user.click(ok)

    await waitFor(() =>
      expect(onSubmit).toHaveBeenCalledWith({
        kind: 'physical',
        name: undefined,
        activation_code: 'ABC',
        agent_device_id: 'dev_a',
      }),
    )
  })

  it('switches back to a virtual device on request', async () => {
    const user = userEvent.setup()
    const { onSubmit } = await setup({ devices: [makeDevice()] })

    await hardwareForm()
    // antd hides the radio input itself, so click the label text.
    await user.click(screen.getByText('虚拟设备'))
    await user.click(await screen.findByRole('button', { name: /创\s*建/ }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith({ kind: 'virtual', name: undefined }))
  })
})
