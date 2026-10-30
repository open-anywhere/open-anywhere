import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { Brand } from './Brand'
import { renderInRouter } from '../test/router'

function logo(): HTMLImageElement {
  const element = screen.getByAltText('Open Anywhere')
  return element as HTMLImageElement
}

describe('Brand', () => {
  it('links home by default', async () => {
    await renderInRouter(<Brand />)
    expect(screen.getByRole('link', { name: 'Open Anywhere 首页' })).toHaveAttribute('href', '/')
  })

  it('links to the console when asked', async () => {
    await renderInRouter(<Brand to="/portal" />)
    expect(screen.getByRole('link', { name: 'Open Anywhere 首页' })).toHaveAttribute(
      'href',
      '/portal',
    )
  })

  it('uses the dark logo in dark mode', async () => {
    await renderInRouter(<Brand />)
    const light = logo().getAttribute('src')

    window.localStorage.setItem('oa-theme', 'dark')
    await renderInRouter(<Brand />)
    const dark = screen.getAllByAltText('Open Anywhere').at(-1)?.getAttribute('src')

    expect(dark).toBeDefined()
    expect(dark).not.toBe(light)
  })
})
