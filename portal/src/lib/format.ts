export function formatDate(value?: string): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return new Intl.DateTimeFormat('zh-CN', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

export function formatRelative(value?: string): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  const diff = Date.now() - date.getTime()
  if (diff < 0) return formatDate(value)
  if (diff < 60_000) return '刚刚'
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)} 分钟前`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)} 小时前`
  if (diff < 172_800_000) return `昨天 ${formatDate(value).split(' ').slice(-1)[0]}`
  return formatDate(value)
}

export function formatDuration(seconds?: number): string {
  if (!seconds || seconds < 0) return '—'
  if (seconds < 60) return `${Math.round(seconds)} 秒`
  const minutes = Math.floor(seconds / 60)
  const rest = Math.round(seconds % 60)
  if (minutes < 60) return rest ? `${minutes} 分 ${rest} 秒` : `${minutes} 分钟`
  const hours = Math.floor(minutes / 60)
  return `${hours} 小时 ${minutes % 60} 分`
}

export const stepLabels: Record<string, string> = {
  create_agent: '创建智能体',
  attach_mcp: '挂载 MCP',
  ota: '注册设备',
  bind: '绑定设备',
  token: '获取凭证',
  done: '完成',
}

export const appStateLabels: Record<string, string> = {
  building: '构建中',
  idle: '待启动',
  running: '屏幕上',
  error: '失败',
}

export const deviceStateLabels: Record<string, string> = {
  registered: '已注册',
  provisioning: '开通中',
  error: '失败',
  disabled: '已停用',
}

export const voiceStateLabels: Record<string, string> = {
  connecting: '连接中',
  listening: '收听中',
  thinking: '思考中',
  speaking: '播报中',
  idle: '已暂停',
  reconnecting: '重连中',
  disconnected: '已断开',
  offline: '未启动',
  error: '异常',
}

export const screenStatusLabels: Record<string, string> = {
  free: '空闲',
  showing_app: '正在展示',
  external: '被其他投屏占用',
  unavailable: '不可用',
}
