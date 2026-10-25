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
        <Brand subtitle="Portal" />
        <div>
          <span className="eyebrow">Your agent, your screen</span>
          <h1>
            登录之后，
            <br />
            智能体和屏幕都会
            <br />
            为这场活动准备好。
          </h1>
          <p>
            注册之后，系统会自动创建并开通你的智能体。你只需要开口描述想做的事，
            应用就会出现在身边的屏幕上。
          </p>
          <div className="login-points">
            <span>注册即获得一个专属智能体</span>
            <span>在 Portal 里直接发起语音通话</span>
            <span>查看应用、屏幕、服务与活动时间线</span>
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
          <p className="sub">管理员和普通用户使用同一个入口登录。</p>
          {switching && me.data?.user ? (
            <p className="sub">
              当前已登录为 {me.data.user.username}。登录其他账号会替换当前会话。
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
            还没有账号？<Link to="/register">免费注册</Link>
          </p>
        </div>
      </section>
    </div>
  )
}
