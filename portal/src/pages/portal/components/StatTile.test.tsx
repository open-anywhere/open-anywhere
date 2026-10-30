import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { StatTile } from './StatTile'

describe('StatTile', () => {
  it('renders a label and a value', () => {
    render(<StatTile label="应用" value={3} />)
    expect(screen.getByText('应用')).toHaveClass('stat-label')
    expect(screen.getByText('3')).toHaveClass('stat-value')
    expect(document.querySelector('.stat-icon')).toBeNull()
    expect(document.querySelector('.stat-note')).toBeNull()
  })

  it('renders an icon with its tone and a note', () => {
    render(
      <StatTile
        icon={<span>i</span>}
        label="屏幕"
        value="2"
        note="1 台在线"
        tone="success"
      />,
    )

    expect(screen.getByText('i').parentElement).toHaveClass('stat-icon-success')
    expect(screen.getByText('1 台在线')).toHaveClass('stat-note')
  })
})
