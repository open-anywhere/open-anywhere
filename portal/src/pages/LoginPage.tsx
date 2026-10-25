import { App as AntApp, Alert, Button, Form, Input } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useRouter, useSearch } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { Brand } from '../components/Brand'
import { ThemeToggle } from '../components/ThemeToggle'
import { ApiError, api } from '../api'
import { meQueryOptions } from '../api/queries'

interface FormValues {
  username: string
  password: string
}

function safeRedirect(value?: string): string | undefined {
  if (!value || !value.startsWith('/') || value.startsWith('//')) return undefined
  if (value.startsWith('/login') || value.startsWith('/register')) return undefined
  return value
}

export function LoginPage() {
  const [error, setError] = useState<string>()
  const queryClient = useQueryClient()
  const router = useRouter()
  const search = useSearch({ from: '/login' })
  const { message } = AntApp.useApp()
  const me = useQuery(meQueryOptions)
  const requested = safeRedirect(search.redirect)
  const switching = Boolean(search.switchAccount)
  const homeFor = (role?: string) => requested ?? (role === 'admin' ? '/admin' : '/portal')

  useEffect(() => {
    if (!switching && me.data?.user) {
      router.history.replace(homeFor(me.data.user.role))
    }
  }, [me.data, router, requested, switching])

  const mutation = useMutation({
    mutationFn: (values: FormValues) => api.login(values.username.trim(), values.password),
    onSuccess: async () => {
      queryClient.removeQueries({ queryKey: ['me'] })
      const next = await queryClient.fetchQuery(meQueryOptions)
      queryClient.invalidateQueries({ queryKey: ['apps'] })
      queryClient.invalidateQueries({ queryKey: ['screens'] })
      queryClient.invalidateQueries({ queryKey: ['services'] })
      message.success('已登录')
      router.history.replace(homeFor(next.user.role))
    },
    onError: (err) => {
      setError(err instanceof ApiError ? err.message : '登录失败，请稍后重试')
    },
  })

  const submit = (values: FormValues) => {
    setError(undefined)
    mutation.mutate(values)
  }

  return (
    <div className="login-page">
      <section className="login-story">
        <Brand />
        <div>
          <span className="eyebrow">说出需要 · 现场生成 · 抬头参与</span>
          <h1>
            一句话，
            <br />
            让这一刻的现场
            <br />
            亮起来。
          </h1>
          <p>
            朋友聚会、家庭时光、团队讨论。只要你说清楚此刻想做什么，
            应用就会现做现用，出现在身边的屏幕上。
          </p>
          <div className="login-points">
            <span>不用下载与安装，有块屏幕就能开场</span>
            <span>说一句话，应用当场生成</span>
            <span>用声音主持全程，大家抬头就能参与</span>
          </div>
        </div>
        <Link to="/" className="login-back">
          ← 返回首页
        </Link>
      </section>

      <section className="login-panel">
        <div className="login-theme">
          <ThemeToggle />
        </div>
        <div className="login-card">
          <h2>欢迎回来</h2>
          <p className="sub">登录后，继续你的下一场活动。</p>
          {switching && me.data?.user ? (
            <p className="sub">
              当前登录的是 {me.data.user.username}。继续登录会替换当前账号。
            </p>
          ) : null}

          {error ? (
            <Alert
              type="error"
              showIcon
              message={error}
              style={{ marginBottom: 18 }}
              closable
              onClose={() => setError(undefined)}
            />
          ) : null}

          <Form layout="vertical" requiredMark={false} onFinish={submit} size="large">
            <Form.Item
              label="用户名"
              name="username"
              rules={[
                { required: true, message: '请输入用户名' },
                { min: 3, max: 32, message: '用户名长度需在 3–32 个字符之间' },
                {
                  pattern: /^[A-Za-z0-9_.-]+$/,
                  message: '只能包含字母、数字、下划线、连字符和点',
                },
              ]}
            >
              <Input placeholder="你的用户名" autoComplete="username" autoFocus />
            </Form.Item>
            <Form.Item
              label="密码"
              name="password"
              rules={[
                { required: true, message: '请输入密码' },
                { min: 8, max: 128, message: '密码长度需在 8–128 个字符之间' },
              ]}
            >
              <Input.Password placeholder="你的密码" autoComplete="current-password" />
            </Form.Item>
            <Button
              type="primary"
              htmlType="submit"
              size="large"
              block
              loading={mutation.isPending}
              style={{ marginTop: 4 }}
            >
              登录
            </Button>
          </Form>

          <p className="login-switch">
            还没有账号？<Link to="/register">免费开始</Link>
          </p>
        </div>
      </section>
    </div>
  )
}
