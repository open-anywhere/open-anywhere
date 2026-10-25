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
      message.success('注册成功，正在为你创建智能体')
      navigate({ to: '/portal', replace: true })
    },
    onError: (err) => {
      setError(err instanceof ApiError ? err.message : '注册失败，请稍后重试')
    },
  })

  return (
    <div className="login-page">
      <section className="login-story">
        <Brand subtitle="Portal" />
        <div>
          <span className="eyebrow">Start with a voice</span>
          <h1>
            注册之后，
            <br />
            你会得到一个自己的智能体，
            <br />
            和一块随时可用的屏幕。
          </h1>
          <p>
            注册后系统会自动创建智能体，并把它绑定到你的账号。你只需要描述想做的事，
            应用就会生成并投到身边的屏幕上。
          </p>
          <div className="login-points">
            <span>注册后自动创建并开通智能体</span>
            <span>在 Portal 里直接发起语音通话</span>
            <span>绑定自己的 Render 与投屏服务</span>
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
          <p className="sub">注册即可获得一个智能体。管理员可以随时停用账号。</p>

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
              注册并进入 Portal
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
