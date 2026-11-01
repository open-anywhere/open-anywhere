import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { AppRow } from './AppRow'
import { makeApp, makeDevice } from '../../../test/fixtures'
import { renderInRouter } from '../../../test/router'

describe('AppRow', () => {
  it('links to the app detail page', async () => {
    await renderInRouter(<AppRow app={makeApp()} device={makeDevice()} />)
    expect(screen.getByRole('link')).toHaveAttribute('href', '/portal/apps/app_1')
  })

  it('shows the title, summary and metadata', async () => {
    await renderInRouter(<AppRow app={makeApp()} device={makeDevice()} />)

    expect(screen.getByText('电影知识竞赛')).toBeInTheDocument()
    expect(screen.getByText('五个人一起玩的电影抢答')).toBeInTheDocument()
    expect(screen.getByText('客厅')).toBeInTheDocument()
    expect(screen.getByText('2 个动作')).toBeInTheDocument()
    expect(screen.getByText(/生成于/)).toBeInTheDocument()
    expect(screen.getByText('待播放')).toBeInTheDocument()
  })

  it('falls back to the name and then to a placeholder title', async () => {
    await renderInRouter(<AppRow app={makeApp({ title: undefined })} />)
    expect(screen.getByText('movie-quiz')).toBeInTheDocument()

    await renderInRouter(<AppRow app={makeApp({ title: undefined, name: undefined })} />)
    expect(screen.getAllByText('未命名应用')).toHaveLength(1)
  })

  it('describes an app that is still building', async () => {
    await renderInRouter(<AppRow app={makeApp({ state: 'building', progress: '正在生成页面' })} />)
    expect(screen.getByText('正在生成页面')).toBeInTheDocument()
    expect(screen.getByText('生成中')).toBeInTheDocument()
  })

  it('promises a building app will appear on its own', async () => {
    await renderInRouter(<AppRow app={makeApp({ state: 'building' })} />)
    expect(screen.getByText('正在生成，完成后会自动出现在屏幕上')).toBeInTheDocument()
  })

  it('shows why a build failed', async () => {
    await renderInRouter(
      <AppRow
        app={makeApp({
          state: 'error',
          error: { stage: 'build', code: 'VALIDATION', message: '接口校验失败', last_agent_message: '重试一次' },
        })}
      />,
    )
    expect(screen.getByText('接口校验失败')).toBeInTheDocument()
    expect(screen.getByText('未完成')).toBeInTheDocument()
  })

  it('uses the last agent message when there is no error message', async () => {
    await renderInRouter(
      <AppRow app={makeApp({ state: 'error', error: { stage: 'build', code: 'VALIDATION', last_agent_message: '重试一次' } })} />,
    )
    expect(screen.getByText('重试一次')).toBeInTheDocument()
  })

  it('invites a closer look when nothing explains the failure', async () => {
    await renderInRouter(<AppRow app={makeApp({ state: 'error' })} />)
    expect(screen.getByText('生成时遇到了问题，打开详情看看')).toBeInTheDocument()
  })

  it('says when there is no description and no device', async () => {
    await renderInRouter(<AppRow app={makeApp({ summary: undefined, operation_count: 0 })} />)
    expect(screen.getByText('这个应用还没有描述')).toBeInTheDocument()
    expect(screen.getByText('没有关联的智能体')).toBeInTheDocument()
    expect(screen.getByText('还没有动作')).toBeInTheDocument()
  })

  it('marks an app that has not finished building', async () => {
    await renderInRouter(<AppRow app={makeApp({ built_at: undefined })} />)
    expect(screen.getByText('生成中')).toBeInTheDocument()
  })

})
