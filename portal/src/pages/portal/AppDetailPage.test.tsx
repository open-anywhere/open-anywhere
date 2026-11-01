import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { fail, mockApi, ok } from '../../test/http'
import { makeAppDetail, makeDevice, makeUser } from '../../test/fixtures'
import { renderApp, settledAt } from '../../test/router'

function routes(detail = makeAppDetail(), overrides: Record<string, unknown> = {}) {
  return {
    'GET /api/portal/apps/app_1': ok({ app: detail }),
    'GET /api/portal/me': ok({ user: makeUser(), devices: [makeDevice()] }),
    'GET /api/portal/apps/app_1/openapi.yaml': { text: 'openapi: 3.0.0' },
    'DELETE /api/portal/apps/app_1': ok(null, 204),
    'GET /api/portal/apps': ok({ apps: [] }),
    'GET /api/portal/publications': ok({ publications: [] }),
    'PUT /api/portal/apps/app_1/publication': ok({ publication: { store_id: 'app_1' } }),
    'DELETE /api/portal/apps/app_1/publication': ok(null, 204),
    ...overrides,
  }
}

describe('AppDetailPage', () => {
  it('describes the app, its agent and its operations', async () => {
    mockApi(routes())
    await renderApp('/portal/apps/app_1')

    expect(await screen.findByRole('heading', { name: '电影知识竞赛' })).toBeInTheDocument()
    expect(screen.getByText('五个人一起玩的电影抢答')).toBeInTheDocument()
    expect(screen.getByText('小灵')).toBeInTheDocument()
    expect(screen.getByText('待播放', { selector: '.pill' })).toBeInTheDocument()
    expect(screen.getByText('POST')).toBeInTheDocument()
    expect(screen.getByText('GET')).toBeInTheDocument()
    expect(screen.getByText('/next-question')).toBeInTheDocument()
    expect(screen.getByText('出下一题')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /返回应用/ })).toHaveAttribute('href', '/portal/apps')

    const download = screen.getByRole('link', { name: /下\s*载/ })
    expect(download).toHaveAttribute(
      'href',
      '/api/portal/apps/app_1/openapi.yaml?download=1',
    )
    expect(download).toHaveAttribute('download', 'openapi.yaml')
  })

  it('shows the OpenAPI document on request', async () => {
    const user = userEvent.setup()
    mockApi(routes())
    await renderApp('/portal/apps/app_1')

    await user.click(
      (
        await screen.findAllByRole('button', { name: /接口文档/ }, { timeout: 5_000 })
      )[0],
    )

    expect(await screen.findByText('openapi: 3.0.0', {}, { timeout: 5_000 })).toBeInTheDocument()
    expect(
      await screen.findByRole('link', { name: /下载 YAML/ }, { timeout: 5_000 }),
    ).toBeInTheDocument()
  })

  it('explains a failed OpenAPI read', async () => {
    const user = userEvent.setup()
    mockApi(
      routes(makeAppDetail(), {
        'GET /api/portal/apps/app_1/openapi.yaml': fail(404, 'OPENAPI_UNAVAILABLE', '没有接口文档'),
      }),
    )
    await renderApp('/portal/apps/app_1')

    await user.click(
      (
        await screen.findAllByRole('button', { name: /接口文档/ }, { timeout: 5_000 })
      )[0],
    )
    expect(
      await screen.findByText('暂时无法读取接口文档', {}, { timeout: 5_000 }),
    ).toBeInTheDocument()
    expect(await screen.findByText('没有接口文档', {}, { timeout: 5_000 })).toBeInTheDocument()
  })

  it('keeps a building app from being deleted', async () => {
    mockApi(routes(makeAppDetail({ state: 'building', progress: '正在写代码', operations: [] })))
    await renderApp('/portal/apps/app_1')

    expect(await screen.findByText('正在生成应用')).toBeInTheDocument()
    expect(screen.getByText('正在写代码')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /删\s*除/ })).toBeDisabled()
    expect(screen.getByText('生成完成后会列出它能做什么。')).toBeInTheDocument()
  })

  it('names the stage that failed', async () => {
    mockApi(
      routes(
        makeAppDetail({
          state: 'error',
          error: { stage: 'starting', code: 'START_FAILED', message: '屏幕没有响应' },
        }),
      ),
    )
    await renderApp('/portal/apps/app_1')

    expect(await screen.findByText('播放失败')).toBeInTheDocument()
    expect(screen.getByText('屏幕没有响应')).toBeInTheDocument()
  })

  it('renders a missing app as not found', async () => {
    mockApi(
      routes(makeAppDetail(), {
        'GET /api/portal/apps/app_1': fail(404, 'NOT_FOUND', '没有这个应用'),
      }),
    )
    const { router } = await renderApp('/portal/apps/app_1')

    expect(await screen.findByText('应用不存在')).toBeInTheDocument()
    expect(screen.getByText('没有这个应用')).toBeInTheDocument()
    await userEvent.setup().click(screen.getByRole('button', { name: '返回应用列表' }))
    await settledAt(router, '/portal/apps')
  })

  it('deletes the app after confirmation', async () => {
    const user = userEvent.setup()
    const http = mockApi(routes())
    const { router } = await renderApp('/portal/apps/app_1')

    // Text lookups are far cheaper than accessible-name queries on this page.
    await user.click((await screen.findAllByText(/^删\s*除$/))[0])
    expect(await screen.findByText('删除这个应用？')).toBeInTheDocument()
    const confirms = await screen.findAllByText(/^删\s*除$/)
    await user.click(confirms.at(-1) as HTMLElement)

    await waitFor(() =>
      expect(http.callsFor('DELETE', '/api/portal/apps/app_1')).toHaveLength(1),
    )
    await settledAt(router, '/portal/apps')
  })

  it('publishes the app to the store for review', async () => {
    const user = userEvent.setup()
    const http = mockApi(routes())
    await renderApp('/portal/apps/app_1')

    // The action carries an icon, whose aria-label becomes part of the
    // accessible name; the visible text is the stable handle.
    await user.click((await screen.findAllByText('公开到商城', {}, { timeout: 5_000 }))[0])
    const title = await screen.findByLabelText('标题')
    await user.clear(title)
    await user.type(title, '周末答题夜')
    await user.type(screen.getByLabelText('一句介绍'), '适合家庭聚会')
    await user.click(screen.getByRole('button', { name: '提交审核' }))

    await waitFor(() => {
      expect(http.callsFor('PUT', '/api/portal/apps/app_1/publication')[0]?.body).toEqual({
        title: '周末答题夜',
        summary: '适合家庭聚会',
      })
    })
  })

  it('shows the review state and can unpublish', async () => {
    const user = userEvent.setup()
    const http = mockApi(
      routes(makeAppDetail(), {
        'GET /api/portal/publications': ok({
          publications: [
            {
              store_id: 'app_1',
              title: '电影知识竞赛',
              summary: '五人抢答',
              operation_count: 2,
              published_at: '2026-10-02T10:00:00Z',
              updated_at: '2026-10-02T10:00:00Z',
              status: 'approved',
            },
          ],
        }),
      }),
    )
    await renderApp('/portal/apps/app_1')

    expect(await screen.findByText('商城中')).toBeInTheDocument()
    const unpublish = await screen.findAllByRole('button', { name: /下\s*架/ }, { timeout: 5_000 })
    await user.click(unpublish[0])
    const confirms = await screen.findAllByRole('button', { name: /下\s*架/ })
    await user.click(confirms.at(-1) as HTMLElement)

    await waitFor(() => {
      expect(http.callsFor('DELETE', '/api/portal/apps/app_1/publication')).toHaveLength(1)
    })
  })
})
