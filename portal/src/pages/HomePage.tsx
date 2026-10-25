import { Button } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { Brand } from '../components/Brand'
import { ThemeToggle } from '../components/ThemeToggle'
import { meQueryOptions } from '../api/queries'

const steps = [
  {
    num: '01',
    title: '说出此刻的需要',
    body: '几个人、什么主题、想怎么玩。像和身边的朋友说话一样，把场景说清楚就好。',
    note: '一句话里可以有人数、主题、题量和玩法。',
  },
  {
    num: '02',
    title: '应用当场出现',
    body: 'AI 为这个场合写出题目、画面和规则，做成一个真正能玩、能操作的应用。',
    note: '不用安装，也不用部署。',
  },
  {
    num: '03',
    title: '抬头一起参与',
    body: '应用出现在电视或投影上。你继续用声音主持、出题、记分，大家抬头就能跟上。',
    note: '语音负责主持，屏幕负责让所有人看见。',
  },
]

const scenes = [
  {
    title: '朋友聚会',
    line: '我们五个人，来个旅行主题的抢答，输的人请奶茶。',
  },
  {
    title: '家庭时光',
    line: '用孩子今天学的动物知识，做一场亲子挑战。',
  },
  {
    title: '团队讨论',
    line: '做个方案对比板，记录大家的意见和最后的选择。',
  },
  {
    title: '门店活动',
    line: '今晚有六桌客人，做一个电影主题的互动活动。',
  },
  {
    title: '教室与培训',
    line: '把这几个知识点做成三组对战的练习。',
  },
  {
    title: '个人日常',
    line: '做一个分步做饭计时板，告诉我下一步该干什么。',
  },
]

const differences = [
  {
    title: '说的是需要，不是操作',
    body: '不用翻菜单、学遥控器。把想做的事说出来，剩下的交给它。',
  },
  {
    title: '一块屏幕，所有人共享',
    body: '题目、进度和结果都在大家看得见的地方，而不是锁在某一个人的手机里。',
  },
  {
    title: '每一场都不一样',
    body: '人数、主题和内容都来自此刻的现场。今天是一场问答，明天可以是一块看板。',
  },
]

export function HomePage() {
  const me = useQuery(meQueryOptions)
  const navigate = useNavigate()
  const authed = Boolean(me.data?.user)

  const start = () => navigate({ to: authed ? '/portal' : '/register' })
  const signIn = () =>
    authed
      ? navigate({ to: '/login', search: { switchAccount: true } })
      : navigate({ to: '/login' })

  return (
    <div className="home">
      <div className="home-inner">
        <header className="home-nav">
          <Brand />
          <nav className="home-nav-links">
            <a href="#how" className="home-nav-anchor">怎么开始</a>
            <a href="#scenes" className="home-nav-anchor">适合的现场</a>
            <ThemeToggle />
            {authed ? (
              <Link to="/portal">我的空间</Link>
            ) : (
              <>
                <Link to="/login">登录</Link>
                <Link to="/register">免费开始</Link>
              </>
            )}
          </nav>
        </header>

        <section className="hero">
          <div>
            <span className="eyebrow">为此刻的现场，造一个应用</span>
            <h1>
              说出你想玩的，
              <br />
              让身边的
              <br />
              <span className="hero-accent">屏幕亮起来</span>。
            </h1>
            <p className="hero-lead">
              一句话，AI 就能为这场聚会、这间教室或这次会议做出一个能玩、能操作的应用，
              投到身边的电视或投影上，再陪大家一起进行下去。
            </p>
            <div className="hero-actions">
              <Button type="primary" size="large" onClick={start}>
                {authed ? '进入我的空间' : '免费开始'}
              </Button>
              <Button size="large" onClick={signIn}>
                {authed ? '切换账号' : '登录'}
              </Button>
            </div>
            <p className="hero-note">不用下载，不用安装，也不用提前准备内容。</p>
          </div>

          <div className="screen-stage" aria-hidden="true">
            <div className="screen-card">
              <div className="screen-topline">
                <span>客厅电视 · 正在播放</span>
                <span className="screen-live">现场生成</span>
              </div>
              <div className="screen-body">
                <p className="screen-quote">“我们五个人，来一场电影知识抢答，你来主持。”</p>
                <div className="screen-title">电影知识竞赛</div>
                <span className="screen-placeholder">第 3 题 / 共 5 题 · 2 : 1</span>
                <div className="screen-bar">
                  <span />
                  <span />
                  <span />
                </div>
              </div>
            </div>
          </div>
        </section>

        <section id="how" className="home-section">
          <div className="feature-head">
            <div>
              <span className="eyebrow">怎么开始</span>
              <h2>从一句话，到一块正在播放的屏幕</h2>
            </div>
            <p>找应用、装软件、学规则都可以省掉。你只要说清楚此刻的场景。</p>
          </div>
          <div className="feature-grid">
            {steps.map((step) => (
              <article className="feature-card" key={step.num}>
                <span className="num">{step.num}</span>
                <h3>{step.title}</h3>
                <p>{step.body}</p>
                <div className="placeholder">{step.note}</div>
              </article>
            ))}
          </div>
        </section>

        <section id="scenes" className="home-section">
          <div className="feature-head">
            <div>
              <span className="eyebrow">适合的现场</span>
              <h2>每一场活动，都可以有自己的应用</h2>
            </div>
            <p>从朋友聚会到门店活动，只要现场需要一块屏幕，就可以现做现用。</p>
          </div>
          <div className="scene-grid">
            {scenes.map((scene) => (
              <article className="scene-card" key={scene.title}>
                <span className="scene-title">{scene.title}</span>
                <p>“{scene.line}”</p>
              </article>
            ))}
          </div>
        </section>

        <section className="home-section">
          <div className="feature-head">
            <div>
              <span className="eyebrow">为什么不一样</span>
              <h2>
                不是又一个应用商店，
                <br />
                而是为此刻造一个应用。
              </h2>
            </div>
            <p>现成的游戏要求你适应它的规则；Open Anywhere 让应用适应你的现场。</p>
          </div>
          <div className="feature-grid">
            {differences.map((item) => (
              <article className="feature-card" key={item.title}>
                <h3>{item.title}</h3>
                <p>{item.body}</p>
              </article>
            ))}
          </div>
        </section>

        <section className="cta-band">
          <div>
            <h2>有块屏幕，就能开场。</h2>
            <p>创建账号，几秒钟后就能说出你的第一个想法。</p>
          </div>
          <div className="cta-actions">
            <Button type="primary" size="large" onClick={start}>
              {authed ? '进入我的空间' : '免费开始'}
            </Button>
            <Button size="large" onClick={signIn}>
              {authed ? '切换账号' : '我已有账号'}
            </Button>
          </div>
        </section>

        <footer className="home-footer">
          <span>Open Anywhere · 说出你的需要，让应用在身边的屏幕上诞生。</span>
          <span>为每一块屏幕，现做一个应用。</span>
        </footer>
      </div>
    </div>
  )
}
