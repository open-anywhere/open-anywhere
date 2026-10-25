import { Button } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { Brand } from '../components/Brand'
import { ThemeToggle } from '../components/ThemeToggle'
import { meQueryOptions } from '../api/queries'

const features = [
  {
    num: '01',
    title: '说出来',
    body: '告诉你身边的智能体想做什么：一场派对游戏、一次家庭问答，或者会议里的一块计时板。',
  },
  {
    num: '02',
    title: '自动生成',
    body: '云端编码智能体把你的描述变成可运行的应用，构建进度会实时出现在 Portal 里。',
  },
  {
    num: '03',
    title: '投上屏幕',
    body: '应用生成后投到身边的电视或投影上，语音继续主持流程、操作画面、播报结果。',
  },
]

export function HomePage() {
  const me = useQuery(meQueryOptions)
  const navigate = useNavigate()
  const authed = Boolean(me.data?.user)

  return (
    <div className="home">
      <div className="home-inner">
        <header className="home-nav">
          <Brand />
          <nav className="home-nav-links">
            <a href="#how">产品介绍</a>
            <ThemeToggle />
            {authed ? (
              <Link to="/portal">Portal</Link>
            ) : (
              <>
                <Link to="/register">注册</Link>
                <Link to="/login">登录</Link>
              </>
            )}
          </nav>
        </header>

        <section className="hero">
          <div>
            <span className="eyebrow">Spoken intent · Screen presence</span>
            <h1>
              说出你想玩的，
              <br />
              让身边的<span className="hero-accent">屏幕亮起来</span>。
            </h1>
            <p className="hero-lead">
              用一句话生成互动应用，投到身边的电视、投影或任意一块屏幕上，再和大家一起玩起来。
              不需要下载、安装，也不用提前准备内容。
            </p>
            <div className="hero-actions">
              <Button
                type="primary"
                size="large"
                onClick={() => navigate({ to: authed ? '/portal' : '/register' })}
              >
                {authed ? '进入 Portal' : '免费注册'}
              </Button>
              <Button
                size="large"
                onClick={() =>
                  authed
                    ? navigate({ to: '/login', search: { switchAccount: true } })
                    : navigate({ to: '/login' })
                }
              >
                {authed ? '切换账号' : '登录'}
              </Button>
            </div>
            <p className="hero-note">注册后会为你自动创建一个智能体。首页介绍内容仍在完善中。</p>
          </div>

          <div className="screen-stage" aria-hidden="true">
            <div className="screen-card">
              <div className="screen-topline">
                <span>Screen · Living room</span>
                <span className="screen-live">casting</span>
              </div>
              <div className="screen-body">
                <div className="screen-title">一句话之后，屏幕已经准备好了。</div>
                <span className="screen-placeholder">产品演示占位 · 稍后补充</span>
                <div className="screen-bar">
                  <span />
                  <span />
                  <span />
                </div>
              </div>
            </div>
          </div>
        </section>

        <section id="how">
          <div className="feature-head">
            <div>
              <span className="eyebrow">How it works</span>
              <h2>从一句话到一块正在播放的屏幕</h2>
            </div>
            <p>
              这里先保留三段介绍的位置。等产品文案定稿后，再补上真实的场景描述、演示画面和使用示例。
            </p>
          </div>
          <div className="feature-grid">
            {features.map((feature) => (
              <article className="feature-card" key={feature.num}>
                <span className="num">{feature.num}</span>
                <h3>{feature.title}</h3>
                <p>{feature.body}</p>
                <div className="placeholder">详细介绍占位，暂不实现</div>
              </article>
            ))}
          </div>
        </section>

        <footer className="home-footer">
          <span>Open Anywhere · 让屏幕成为现场的一部分</span>
          <span>
            Portal 入口：{authed ? <Link to="/portal">/portal</Link> : <Link to="/register">/register</Link>}
          </span>
        </footer>
      </div>
    </div>
  )
}
