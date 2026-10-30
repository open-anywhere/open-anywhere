import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { PageHeader } from './PageHeader'

describe('PageHeader', () => {
  it('renders only the title', () => {
    render(<PageHeader title="应用" />)
    expect(screen.getByRole('heading', { level: 1, name: '应用' })).toBeInTheDocument()
    expect(document.querySelector('.eyebrow')).toBeNull()
    expect(document.querySelector('.page-header-actions')).toBeNull()
  })

  it('renders the eyebrow, description and actions', () => {
    render(
      <PageHeader
        eyebrow="我的空间"
        title="应用"
        description="所有在这台设备上生成过的应用。"
        actions={<button type="button">新建</button>}
      />,
    )

    expect(screen.getByText('我的空间')).toHaveClass('eyebrow')
    expect(screen.getByText('所有在这台设备上生成过的应用。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '新建' })).toBeInTheDocument()
  })
})
