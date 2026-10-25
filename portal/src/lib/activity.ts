import type { AppSummary, Device } from '../api'

export type ActivityKind =
  | 'device-created'
  | 'device-registered'
  | 'app-build-started'
  | 'app-built'
  | 'app-started'
  | 'app-stopped'

export interface ActivityItem {
  id: string
  at: string
  kind: ActivityKind
  title: string
  detail?: string
  appId?: string
  deviceId?: string
}

function appTitle(app: AppSummary): string {
  return app.title || app.name || '未命名应用'
}

export function deriveActivity(devices: Device[], apps: AppSummary[]): ActivityItem[] {
  const items: ActivityItem[] = []
  for (const device of devices) {
    items.push({
      id: `device-created-${device.id}`,
      at: device.created_at,
      kind: 'device-created',
      title: '智能体已创建',
      detail: device.agent_name || '你的智能体',
      deviceId: device.id,
    })
    if (device.registered_at) {
      items.push({
        id: `device-registered-${device.id}`,
        at: device.registered_at,
        kind: 'device-registered',
        title: '智能体准备好了',
        detail: device.agent_name || '你的智能体',
        deviceId: device.id,
      })
    }
  }
  for (const app of apps) {
    const name = appTitle(app)
    if (app.build_started_at) {
      items.push({
        id: `app-build-started-${app.app_id}`,
        at: app.build_started_at,
        kind: 'app-build-started',
        title: `开始生成「${name}」`,
        appId: app.app_id,
      })
    }
    if (app.built_at) {
      items.push({
        id: `app-built-${app.app_id}`,
        at: app.built_at,
        kind: 'app-built',
        title: `「${name}」生成完成`,
        appId: app.app_id,
      })
    }
    if (app.started_at) {
      items.push({
        id: `app-started-${app.app_id}`,
        at: app.started_at,
        kind: 'app-started',
        title: `「${name}」开始播放`,
        appId: app.app_id,
      })
    }
    if (app.stopped_at) {
      items.push({
        id: `app-stopped-${app.app_id}`,
        at: app.stopped_at,
        kind: 'app-stopped',
        title: `「${name}」停止播放`,
        appId: app.app_id,
      })
    }
  }
  return items.sort((left, right) => new Date(right.at).getTime() - new Date(left.at).getTime())
}
