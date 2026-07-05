import { reactive } from 'vue'

export interface ToastItem {
  id: number
  kind: 'success' | 'error'
  message: string
}

const queue = reactive<ToastItem[]>([])
let seq = 0

function push(kind: ToastItem['kind'], message: string) {
  const id = ++seq
  queue.push({ id, kind, message })
  setTimeout(() => {
    const i = queue.findIndex(t => t.id === id)
    if (i >= 0) queue.splice(i, 1)
  }, 3500)
}

export function useToast() {
  return {
    success: (message: string) => push('success', message),
    error: (message: string) => push('error', message),
  }
}

export function toastQueue() {
  return queue
}
