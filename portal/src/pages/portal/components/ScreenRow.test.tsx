import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { ScreenRow } from './ScreenRow'
import { makeScreen } from '../../../test/fixtures'
import { renderInRouter } from '../../../test/router'

const playing = { app_id: 'app_1', name: '电影知识竞赛' }

describe('ScreenRow', () => {
  it('marks an online screen and links the app it plays', async () => {
    await renderInRouter(
      <ScreenRow screen={makeScreen({ status: 'showing_app', current_app: playing })} />,
    )

    expect(screen.getByLabelText('在线')).toHaveClass('online')
    expect(screen.getByText('客厅电视')).toBeInTheDocument()
    expect(screen.getByText('正在播放', { selector: '.pill' })).toHaveClass('pill-success')
    const link = screen.getByRole('link', { name: '电影知识竞赛' })
    expect(link).toHaveAttribute('href', '/portal/apps/app_1')
  })

  it('falls back to the screen id and says nothing is playing', async () => {
    await renderInRouter(<ScreenRow screen={makeScreen({ name: undefined })} />)
    expect(screen.getByText('screen_1')).toBeInTheDocument()
    expect(screen.getByText('没有正在播放的内容')).toBeInTheDocument()
  })

  it('explains that another source owns the screen', async () => {
    await renderInRouter(<ScreenRow screen={makeScreen({ status: 'external' })} />)
    expect(screen.getByText('正被其他内容占用')).toBeInTheDocument()
  })

  it('shows an offline screen as offline', async () => {
    await renderInRouter(<ScreenRow screen={makeScreen({ online: false, status: 'unavailable' })} />)
    expect(screen.getByLabelText('离线')).toHaveClass('offline')
    expect(screen.getByText('离线', { selector: '.pill' })).toBeInTheDocument()
  })

  it('names a nameless app in the current app link', async () => {
    await renderInRouter(
      <ScreenRow screen={makeScreen({ status: 'showing_app', current_app: { app_id: 'app_1', name: '' } })} />,
    )
    expect(screen.getByRole('link', { name: '应用' })).toBeInTheDocument()
  })

  it('offers to stop the app only when a handler is given', async () => {
    await renderInRouter(
      <ScreenRow screen={makeScreen({ status: 'showing_app', current_app: playing })} />,
    )
    expect(screen.queryByRole('button', { name: /停止播放/ })).toBeNull()
  })

  it('stops playback after confirmation', async () => {
    const user = userEvent.setup()
    const onStop = vi.fn()
    await renderInRouter(
      <ScreenRow
        screen={makeScreen({ status: 'showing_app', current_app: playing })}
        onStop={onStop}
      />,
    )

    await user.click(screen.getByRole('button', { name: /停止播放/ }))
    expect(await screen.findByText('停止播放？')).toBeInTheDocument()
    expect(screen.getByText(/会停下来，之后还可以再让它播放/)).toBeInTheDocument()
    expect(onStop).not.toHaveBeenCalled()

    const buttons = await screen.findAllByRole('button', { name: /停止播放/ })
    await user.click(buttons.at(-1) as HTMLElement)
    expect(onStop).toHaveBeenCalledTimes(1)
  })

  it('shows the stop button busy while stopping', async () => {
    await renderInRouter(
      <ScreenRow
        screen={makeScreen({ status: 'showing_app', current_app: playing })}
        onStop={() => undefined}
        stopping
      />,
    )

    expect(screen.getByRole('button', { name: /停止播放/ })).toHaveClass('ant-btn-loading')
  })
})
