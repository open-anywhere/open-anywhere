import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { ActivityEntry, ActivityGroupTitle } from './ActivityEntry'
import type { ActivityItem } from '../../../lib/activity'
import { renderInRouter } from '../../../test/router'

function item(overrides: Partial<ActivityItem> = {}): ActivityItem {
  return {
    id: 'device-created-dev_1',
    at: new Date().toISOString(),
    kind: 'device-created',
    title: '智能体已创建',
    ...overrides,
  }
}

describe('ActivityEntry', () => {
  it('links an app event to its detail page', async () => {
    await renderInRouter(
      <ActivityEntry
        item={item({
          kind: 'app-built',
          title: '「电影知识竞赛」生成完成',
          appId: 'app_1',
          detail: '五个人一起玩',
        })}
      />,
    )

    const link = screen.getByRole('link')
    expect(link).toHaveAttribute('href', '/portal/apps/app_1')
    expect(screen.getByText('「电影知识竞赛」生成完成')).toBeInTheDocument()
    expect(screen.getByText('五个人一起玩')).toBeInTheDocument()
  })

  it('links a device event to the agents page', async () => {
    await renderInRouter(<ActivityEntry item={item({ deviceId: 'dev_1' })} />)
    expect(screen.getByRole('link')).toHaveAttribute('href', '/portal/agents')
  })

  it('renders without a link when the event belongs to nothing', async () => {
    await renderInRouter(<ActivityEntry item={item()} />)
    expect(screen.queryByRole('link')).toBeNull()
    expect(document.querySelector('.activity-item')).toBeInTheDocument()
  })

  it('prefers the app link when an event has both ids', async () => {
    await renderInRouter(<ActivityEntry item={item({ appId: 'app_1', deviceId: 'dev_1' })} />)
    expect(screen.getByRole('link')).toHaveAttribute('href', '/portal/apps/app_1')
  })

  it('shows a relative time with the absolute date in the tooltip', async () => {
    await renderInRouter(<ActivityEntry item={item({ at: new Date().toISOString() })} />)
    const time = document.querySelector('time') as HTMLElement
    expect(time).toHaveTextContent('刚刚')
    expect(time).toHaveAttribute('dateTime')
    expect(time.getAttribute('title')).toBeTruthy()
  })

  it('styles the icon by event kind', async () => {
    await renderInRouter(<ActivityEntry item={item({ kind: 'app-started' })} />)
    expect(document.querySelector('.activity-icon')).toHaveClass('activity-icon-app-started')
  })
})

describe('ActivityGroupTitle', () => {
  it('renders a label with a default icon', async () => {
    await renderInRouter(<ActivityGroupTitle label="今天" />)
    expect(screen.getByText('今天')).toBeInTheDocument()
    expect(document.querySelector('.activity-group-title .anticon')).toBeInTheDocument()
  })

  it('accepts a custom icon', async () => {
    await renderInRouter(<ActivityGroupTitle label="今天" icon={<span>custom-icon</span>} />)
    expect(screen.getByText('custom-icon')).toBeInTheDocument()
  })
})
