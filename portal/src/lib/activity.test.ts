import { describe, expect, it } from 'vitest'
import { deriveActivity } from './activity'
import { makeApp, makeDevice } from '../test/fixtures'

describe('deriveActivity', () => {
  it('returns an empty timeline with no devices or apps', () => {
    expect(deriveActivity([], [])).toEqual([])
  })

  it('creates a device-created entry, and a registered entry once registered', () => {
    const device = makeDevice({ registered_at: undefined })
    expect(deriveActivity([device], []).map((item) => item.kind)).toEqual(['device-created'])

    const registered = makeDevice()
    const items = deriveActivity([registered], [])
    expect(items.map((item) => item.kind)).toEqual(['device-registered', 'device-created'])
    expect(items[0]).toMatchObject({
      id: 'device-registered-dev_1',
      detail: '小灵',
      deviceId: 'dev_1',
    })
  })

  it('falls back to a generic device name', () => {
    const device = makeDevice({ agent_name: '', registered_at: undefined })
    expect(deriveActivity([device], [])[0].detail).toBe('你的智能体')
  })

  it('creates one entry per app milestone', () => {
    const app = makeApp({
      build_started_at: '2026-10-02T10:00:00Z',
      built_at: '2026-10-02T10:01:00Z',
      started_at: '2026-10-02T10:02:00Z',
      stopped_at: '2026-10-02T10:03:00Z',
    })
    const items = deriveActivity([], [app])
    expect(items.map((item) => item.kind)).toEqual([
      'app-stopped',
      'app-started',
      'app-built',
      'app-build-started',
    ])
    expect(items[0].title).toBe('「电影知识竞赛」停止播放')
    expect(items[0].appId).toBe('app_1')
  })

  it('prefers the name when the app has no title', () => {
    const app = makeApp({ title: '', name: 'movie-quiz', built_at: '2026-10-02T10:00:00Z' })
    expect(deriveActivity([], [app])[0].title).toBe('「movie-quiz」生成完成')
  })

  it('uses a placeholder for an unnamed app', () => {
    const app = makeApp({ title: '', name: '', built_at: '2026-10-02T10:00:00Z' })
    expect(deriveActivity([], [app])[0].title).toBe('「未命名应用」生成完成')
  })

  it('orders entries newest first across devices and apps', () => {
    const device = makeDevice({ created_at: '2026-10-01T08:00:00Z', registered_at: undefined })
    const app = makeApp({ built_at: '2026-10-03T08:00:00Z' })
    const items = deriveActivity([device], [app])
    expect(items[0].at).toBe('2026-10-03T08:00:00Z')
    expect(items[1].at).toBe('2026-10-01T08:00:00Z')
  })
})
