import type { AppSummary, Device } from '../api'

export function primaryDevice(devices?: Device[]): Device | undefined {
  if (!devices?.length) return undefined
  return devices.find((device) => device.kind === 'virtual') ?? devices[0]
}

export function findDevice(devices: Device[] | undefined, id?: string): Device | undefined {
  if (!devices?.length) return undefined
  if (id) {
    // An explicit device id must resolve. Falling back to the primary agent
    // would silently start a call with a different agent than the link names.
    return devices.find((device) => device.id === id)
  }
  return primaryDevice(devices)
}

export function deviceLabel(device?: Device): string {
  return device?.name || device?.agent_name || device?.mac || '未命名设备'
}

export function deviceForApp(devices: Device[] | undefined, app: AppSummary): Device | undefined {
  if (!devices?.length) return undefined
  return devices.find(
    (device) =>
      device.mac === app.device_id || device.id === app.device_id || device.agent_id === app.device_id,
  )
}
