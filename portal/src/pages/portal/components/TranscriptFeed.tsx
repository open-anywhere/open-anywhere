import { useEffect, useRef } from 'react'
import type { TranscriptEntry } from '../../../hooks/useVoiceSession'
import { voiceStateLabels } from '../../../lib/format'

const roleLabels: Record<TranscriptEntry['role'], string> = {
  user: '你',
  agent: '智能体',
  system: '状态',
  tool: '工具',
  error: '错误',
}

const typeLabels: Record<string, string> = {
  stt: '语音识别',
  tts: '语音播报',
  llm: '模型',
  mcp: 'MCP',
  state: '状态',
  custom: '事件',
  close: '连接',
  error: '错误',
}

function time(at: number): string {
  return new Intl.DateTimeFormat('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(new Date(at))
}

export function TranscriptFeed({ entries }: { entries: TranscriptEntry[] }) {
  const listRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const element = listRef.current
    if (element) {
      element.scrollTop = element.scrollHeight
    }
  }, [entries.length])

  if (!entries.length) {
    return (
      <div className="transcript-empty">
        <strong>等待通话开始</strong>
        <p>点击下方「开始通话」并允许麦克风权限。之后你和智能体的对话会实时显示在这里。</p>
      </div>
    )
  }

  return (
    <div className="transcript" ref={listRef} aria-live="polite">
      {entries.map((entry) => {
        const isState = entry.type === 'state'
        return (
          <article className={`transcript-item transcript-${entry.role}`} key={entry.id}>
            <div className="transcript-meta">
              <span>{isState ? '通话状态' : roleLabels[entry.role]}</span>
              {isState ? null : <span>{typeLabels[entry.type] ?? entry.type}</span>}
              <time>{time(entry.at)}</time>
            </div>
            <p>{isState ? (voiceStateLabels[entry.text] ?? entry.text) : entry.text}</p>
          </article>
        )
      })}
    </div>
  )
}
