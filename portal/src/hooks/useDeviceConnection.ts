import { useMutation, useQueryClient } from '@tanstack/react-query'
import { App as AntApp } from 'antd'
import { ApiError, api } from '../api'

/**
 * Disconnect and reconnect a device's voice connection to Linx. Both refresh the
 * device list so the voice state shown next to the agent is current.
 */
export function useDeviceConnection() {
  const queryClient = useQueryClient()
  const { message } = AntApp.useApp()
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['me'] })

  const disconnect = useMutation({
    mutationFn: (id: string) => api.disconnectDevice(id),
    onSuccess: () => {
      message.info('已断开连接')
      refresh()
    },
    onError: (error) => {
      message.error(error instanceof ApiError ? error.message : '暂时无法断开连接')
    },
  })

  const reconnect = useMutation({
    mutationFn: (id: string) => api.reconnectDevice(id),
    onSuccess: () => {
      message.success('已重新连接')
      refresh()
    },
    onError: (error) => {
      message.error(error instanceof ApiError ? error.message : '暂时无法重新连接')
    },
  })

  return { disconnect, reconnect }
}
