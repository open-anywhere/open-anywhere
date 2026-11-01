import { ReloadOutlined } from '@ant-design/icons'
import { App as AntApp, Button, Input, Modal, Skeleton, Space, Table, Tag } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { adminApi, ApiError, type AdminStoreItem } from '../../api'
import { usePageTitle } from '../../hooks/usePageTitle'
import { formatDate } from '../../lib/format'
import { PageHeader } from '../portal/components/PageHeader'

const statusLabels: Record<AdminStoreItem['status'], { label: string; color: string }> = {
  pending: { label: '待审核', color: 'gold' },
  approved: { label: '已通过', color: 'green' },
  rejected: { label: '已拒绝', color: 'red' },
}

export function AdminStorePage() {
  usePageTitle('商城审核')
  const queryClient = useQueryClient()
  const { message } = AntApp.useApp()
  const [rejecting, setRejecting] = useState<AdminStoreItem | null>(null)
  const [note, setNote] = useState('')

  const store = useQuery({ queryKey: ['admin', 'store'], queryFn: () => adminApi.store() })

  const review = useMutation({
    mutationFn: (input: { item: AdminStoreItem; decision: 'approve' | 'reject'; note?: string }) =>
      adminApi.reviewStore(input.item.store_id, input.decision, input.note ?? ''),
    onSuccess: async (_result, input) => {
      await queryClient.invalidateQueries({ queryKey: ['admin', 'store'] })
      await queryClient.invalidateQueries({ queryKey: ['store'] })
      message.success(input.decision === 'approve' ? '已通过，应用已进入商城' : '已拒绝，作者会看到审核意见')
      setRejecting(null)
      setNote('')
    },
    onError: (error) => {
      message.error(error instanceof ApiError ? error.message : '操作失败，请稍后重试')
    },
  })

  const items = store.data?.items ?? []

  return (
    <div className="page">
      <PageHeader
        eyebrow="Admin"
        title="商城审核"
        description="公开的应用先进入待审核；通过后所有登录用户都能看到并添加一份副本。"
        actions={
          <Button icon={<ReloadOutlined />} onClick={() => store.refetch()} loading={store.isFetching}>
            刷新
          </Button>
        }
      />

      {store.isLoading ? (
        <Skeleton active />
      ) : (
        <section className="surface">
          <Table<AdminStoreItem>
            rowKey="store_id"
            dataSource={items}
            pagination={false}
            locale={{ emptyText: '还没有人公开应用' }}
            columns={[
              {
                title: '标题',
                dataIndex: 'title',
                render: (_value, item) => (
                  <Space direction="vertical" size={0}>
                    <strong>{item.title}</strong>
                    <span className="muted-text">{item.summary || '未填写简介'}</span>
                  </Space>
                ),
              },
              {
                title: '发布者',
                dataIndex: 'username',
                render: (_value, item) => item.username || item.owner_user_id,
              },
              {
                title: '状态',
                dataIndex: 'status',
                render: (status: AdminStoreItem['status']) => (
                  <Tag color={statusLabels[status].color}>{statusLabels[status].label}</Tag>
                ),
              },
              {
                title: '更新时间',
                dataIndex: 'updated_at',
                render: (value: string) => formatDate(value),
              },
              {
                title: '审核意见',
                dataIndex: 'review_note',
                render: (value: string | undefined) => value || '—',
              },
              {
                title: '操作',
                key: 'actions',
                render: (_value, item) => (
                  <Space>
                    <Button
                      size="small"
                      type="primary"
                      disabled={item.status === 'approved' || review.isPending}
                      onClick={() => review.mutate({ item, decision: 'approve' })}
                    >
                      通过
                    </Button>
                    <Button
                      size="small"
                      danger
                      disabled={item.status === 'rejected' || review.isPending}
                      onClick={() => {
                        setRejecting(item)
                        setNote(item.review_note ?? '')
                      }}
                    >
                      拒绝
                    </Button>
                  </Space>
                ),
              },
            ]}
          />
        </section>
      )}

      <Modal
        open={rejecting !== null}
        title={rejecting ? `拒绝「${rejecting.title}」` : '拒绝公开'}
        okText="确认拒绝"
        cancelText="取消"
        okButtonProps={{ danger: true, loading: review.isPending }}
        onOk={() => {
          if (rejecting) review.mutate({ item: rejecting, decision: 'reject', note: note.trim() })
        }}
        onCancel={() => setRejecting(null)}
      >
        <Input.TextArea
          rows={3}
          maxLength={200}
          value={note}
          placeholder="写给作者的原因（可选）"
          onChange={(event) => setNote(event.target.value)}
        />
      </Modal>
    </div>
  )
}
