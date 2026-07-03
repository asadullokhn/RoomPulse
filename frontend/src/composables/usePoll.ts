import { onMounted, onUnmounted } from 'vue'

// Runs `fn` immediately, then every `intervalMs`, until the owning component unmounts.
export function usePoll(fn: () => void | Promise<void>, intervalMs: number) {
  let timer: ReturnType<typeof setInterval> | undefined

  onMounted(() => {
    void fn()
    timer = setInterval(() => void fn(), intervalMs)
  })

  onUnmounted(() => {
    if (timer) clearInterval(timer)
  })
}
