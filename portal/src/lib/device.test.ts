import { describe, expect, it } from 'vitest'
import { deviceForApp, deviceLabel, findDevice, primaryDevice } from './device'
import { makeApp, makeDevice } from '../test/fixtures'

describe('primaryDevice', () => {
  it('returns undefined without devices', () => {
    expect(primaryDevice()).toBeUndefined()
    expect(primaryDevice([])).toBeUndefined()
  })

  it('prefers the virtual device', () => {
    const physical = makeDevice({ id: 'hw', kind: 'physical' })
    const virtual = makeDevice({ id: 'virt', kind: 'virtual' })
    expect(primaryDevice([physical, virtual])?.id).toBe('virt')
  })

  it('falls back to the first device when only hardware exists', () => {
    const physical = makeDevice({ id: 'hw', kind: 'physical' })
    expect(primaryDevice([physical])?.id).toBe('hw')
  })
})

describe('findDevice', () => {
  const first = makeDevice({ id: 'dev_1' })
  const second = makeDevice({ id: 'dev_2', kind: 'physical' })

  it('returns undefined without devices', () => {
    expect(findDevice(undefined, 'dev_1')).toBeUndefined()
    expect(findDevice([], 'dev_1')).toBeUndefined()
  })

  it('resolves an explicit id exactly', () => {
    expect(findDevice([first, second], 'dev_2')?.id).toBe('dev_2')
  })

  it('never silently substitutes another device for an unknown id', () => {
    expect(findDevice([first, second], 'missing')).toBeUndefined()
  })

  it('falls back to the primary device when no id is given', () => {
    expect(findDevice([second, first])?.id).toBe('dev_1')
  })
})

describe('deviceLabel', () => {
  it('prefers name, then agent name, then mac', () => {
    expect(deviceLabel(makeDevice({ name: '客厅' }))).toBe('客厅')
    expect(deviceLabel(makeDevice({ name: '', agent_name: '小灵' }))).toBe('小灵')
    expect(deviceLabel(makeDevice({ name: '', agent_name: '', mac: 'AA:BB' }))).toBe('AA:BB')
    expect(deviceLabel()).toBe('未命名设备')
  })
})

describe('deviceForApp', () => {
  const app = makeApp()

  it('returns undefined without devices', () => {
    expect(deviceForApp(undefined, app)).toBeUndefined()
  })

  it('matches on mac, device id or agent id', () => {
    const byMac = makeDevice({ mac: 'AA:BB:CC:DD:EE:01' })
    const byId = makeDevice({ id: 'AA:BB:CC:DD:EE:01', mac: 'other' })
    const byAgent = makeDevice({ id: 'x', mac: 'y', agent_id: 'AA:BB:CC:DD:EE:01' })
    expect(deviceForApp([byMac], app)?.id).toBe(byMac.id)
    expect(deviceForApp([byId], app)?.id).toBe(byId.id)
    expect(deviceForApp([byAgent], app)?.id).toBe(byAgent.id)
  })

  it('returns undefined when nothing matches', () => {
    expect(deviceForApp([makeDevice({ mac: 'zzz', id: 'zzz', agent_id: 'zzz' })], app)).toBeUndefined()
  })
})
