import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { TranscriptFeed } from './TranscriptFeed'
import type { TranscriptEntry } from '../../../hooks/useVoiceSession'

function entry(overrides: Partial<TranscriptEntry>): TranscriptEntry {
  return {
    id: 1,
    type: 'stt',
    role: 'user',
    text: '来一个抢答游戏',
    at: new Date('2026-10-30T10:00:00Z').getTime(),
    ...overrides,
  }
}

describe('TranscriptFeed', () => {
  it('shows a hint while the conversation has not started', () => {
    render(<TranscriptFeed entries={[]} />)
    expect(screen.getByText('等待对话开始')).toBeInTheDocument()
    expect(screen.queryByRole('article')).toBeNull()
  })

  it('labels each entry by role and event type', () => {
    render(
      <TranscriptFeed
        entries={[
          entry({ id: 1, type: 'stt', role: 'user', text: '来一个抢答游戏' }),
          entry({ id: 2, type: 'tts', role: 'agent', text: '好的' }),
          entry({ id: 3, type: 'mcp', role: 'tool', text: '正在执行：start_app' }),
          entry({ id: 4, type: 'custom', role: 'system', text: '自定义事件' }),
          entry({ id: 5, type: 'error', role: 'error', text: '出错了' }),
        ]}
      />,
    )

    expect(screen.getByText('你')).toBeInTheDocument()
    expect(screen.getByText('听到')).toBeInTheDocument()
    expect(screen.getByText('智能体')).toBeInTheDocument()
    expect(screen.getByText('回答')).toBeInTheDocument()
    expect(screen.getByText('动作')).toBeInTheDocument()
    expect(screen.getByText('操作')).toBeInTheDocument()
    expect(screen.getByText('状态')).toBeInTheDocument()
    expect(screen.getByText('事件')).toBeInTheDocument()
    // The error entry uses the same word for its role and its type.
    expect(screen.getAllByText('提示')).toHaveLength(2)
  })

  it('translates a state entry and hides its type label', () => {
    render(<TranscriptFeed entries={[entry({ id: 1, type: 'state', role: 'system', text: 'thinking' })]} />)

    expect(screen.getByText('对话状态')).toBeInTheDocument()
    expect(screen.getByText('思考中')).toBeInTheDocument()
    // Only the status label is left in the meta row.
    const meta = document.querySelector('.transcript-meta') as HTMLElement
    expect(meta.children).toHaveLength(2)
  })

  it('keeps an unknown state readable', () => {
    render(
      <TranscriptFeed entries={[entry({ id: 1, type: 'state', role: 'system', text: 'warping' })]} />,
    )
    expect(screen.getByText('warping')).toBeInTheDocument()
  })

  it('tags each entry with its role for styling', () => {
    render(<TranscriptFeed entries={[entry({ id: 1, role: 'agent', type: 'tts' })]} />)
    expect(document.querySelector('.transcript-item')).toHaveClass('transcript-agent')
  })
})
