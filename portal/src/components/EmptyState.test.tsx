import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { EmptyState } from './EmptyState'

describe('EmptyState', () => {
  it('renders the title alone', () => {
    render(<EmptyState title="还没有应用" />)
    expect(screen.getByText('还没有应用')).toBeInTheDocument()
    expect(document.querySelector('.empty-state-icon')).toBeNull()
    expect(document.querySelector('.empty-state-action')).toBeNull()
  })

  it('renders an icon, a description and an action', () => {
    render(
      <EmptyState
        icon={<span>icon</span>}
        title="还没有应用"
        description="对它说一句话就会生成一个。"
        action={<button type="button">开始</button>}
      />,
    )

    expect(screen.getByText('icon')).toBeInTheDocument()
    expect(screen.getByText('对它说一句话就会生成一个。').tagName).toBe('P')
    expect(screen.getByRole('button', { name: '开始' })).toBeInTheDocument()
  })
})
