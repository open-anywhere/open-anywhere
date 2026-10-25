import { Alert, App as AntApp, Button, Form, Input } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { ApiError, api } from '../api'
import { meQueryOptions } from '../api/queries'
import { Brand } from '../components/Brand'
import { ThemeToggle } from '../components/ThemeToggle'

interface FormValues {
  username: string
  password: string
  confirm: string
}

export function RegisterPage() {
  const [error, setError] = useState<string>()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const { message } = AntApp.useApp()
  const me = useQuery(meQueryOptions)

  useEffect(() => {
    if (me.data?.user) {
      navigate({ to: me.data.user.role === 'admin' ? '/admin' : '/portal', replace: true })
    }
  }, [me.data, navigate])

  const mutation = useMutation({
    mutationFn: (values: FormValues) => api.register(values.username.trim(), values.password),
    onSuccess: async () => {
      queryClient.removeQueries({ queryKey: ['me'] })
      await queryClient.fetchQuery(meQueryOptions)
      queryClient.invalidateQueries({ queryKey: ['services'] })
      message.success('账号已创建，正在准备你的空间')
      navigate({ to: '/portal', replace: true })
    },
    onError: (err) => {
      setError(err instanceof ApiError ? err.message : '注册失败，请稍后重试')
    },
  })

  return (
    <div className="login-page">
      <section className="login-story">
        <Brand />
        <div>
          <span className="eyebrow">从一句话开始</span>
          <h1>
            一个账号，
            <br />
            让每一块屏幕
            <br />
            都有东西可玩。
          </h1>
          <p>
            说出人数、主题和玩法，AI 会为这个场合做出专属的应用，
            投到身边的电视、投影或任意一块屏幕上。
          </p>
          <div className="login-points">
            <span>说出需要，应用当场生成</span>
            <span>投到身边的电视、投影或任意屏幕</span>
            <span>用声音主持、出题、记分，不用学操作</span>
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
          <h2>创建账号</h2>
          <p className="sub">几秒钟后，就能说出你的第一个想法。</p>

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

          <Form layout="vertical" requiredMark={false} onFinish={(values) => {
            setError(undefined)
            mutation.mutate(values)
          }} size="large">
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
              <Input placeholder="例如 ada" autoComplete="username" autoFocus />
            </Form.Item>
            <Form.Item
              label="密码"
              name="password"
              rules={[
                { required: true, message: '请输入密码' },
                { min: 8, max: 128, message: '密码长度需在 8–128 个字符之间' },
              ]}
            >
              <Input.Password placeholder="至少 8 个字符" autoComplete="new-password" />
            </Form.Item>
            <Form.Item
              label="确认密码"
              name="confirm"
              dependencies={['password']}
              rules={[
                { required: true, message: '请再次输入密码' },
                ({ getFieldValue }) => ({
                  validator(_, value) {
                    if (!value || getFieldValue('password') === value) {
                      return Promise.resolve()
                    }
                    return Promise.reject(new Error('两次输入的密码不一致'))
                  },
                }),
              ]}
            >
              <Input.Password placeholder="再次输入密码" autoComplete="new-password" />
            </Form.Item>
            <Button
              type="primary"
              htmlType="submit"
              size="large"
              block
              loading={mutation.isPending}
              style={{ marginTop: 4 }}
            >
              创建账号，开始体验
            </Button>
          </Form>

          <p className="login-switch">
            已经有账号？<Link to="/login">直接登录</Link>
          </p>
        </div>
      </section>
    </div>
  )
}
