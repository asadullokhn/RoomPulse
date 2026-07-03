import { ref } from 'vue'

// Tracks the live/reconnecting chip state shared by all 3 views' fetch loops.
export function useConnection() {
  const connected = ref(false)
  return {
    connected,
    markUp: () => { connected.value = true },
    markDown: () => { connected.value = false },
  }
}
