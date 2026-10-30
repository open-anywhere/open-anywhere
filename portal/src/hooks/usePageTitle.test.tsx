import { describe, expect, it } from 'vitest'
import { render } from '@testing-library/react'
import { usePageTitle } from './usePageTitle'

function Probe({ title }: { title?: string }) {
  usePageTitle(title)
  return null
}

describe('usePageTitle', () => {
  it('uses the product name alone without a title', () => {
    render(<Probe />)
    expect(document.title).toBe('Open Anywhere')
  })

  it('prefixes the page title', () => {
    render(<Probe title="应用" />)
    expect(document.title).toBe('应用 · Open Anywhere')
  })

  it('updates when the title changes', () => {
    const { rerender } = render(<Probe title="应用" />)
    rerender(<Probe title="屏幕" />)
    expect(document.title).toBe('屏幕 · Open Anywhere')
  })
})
