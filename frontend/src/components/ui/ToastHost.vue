<script setup lang="ts">
import { toastQueue } from '@/composables/useToast'

const toasts = toastQueue()
</script>

<template>
  <Teleport to="body">
    <div class="host" aria-live="polite">
      <TransitionGroup name="toast">
        <div v-for="t in toasts" :key="t.id" class="toast" :class="t.kind">
          <span class="dot" />{{ t.message }}
        </div>
      </TransitionGroup>
    </div>
  </Teleport>
</template>

<style scoped>
.host { position: fixed; bottom: 22px; left: 50%; transform: translateX(-50%);
  z-index: 200; display: flex; flex-direction: column; align-items: center; gap: 8px; pointer-events: none; }
.toast { display: flex; align-items: center; gap: 8px; background: rgba(28, 28, 30, .92);
  backdrop-filter: blur(10px); -webkit-backdrop-filter: blur(10px);
  color: #f5f5f7; font-size: 13px; font-weight: 500; padding: 9px 16px; border-radius: 980px;
  box-shadow: 0 6px 24px rgba(0, 0, 0, .22); }
.dot { width: 7px; height: 7px; border-radius: 50%; flex: none; }
.toast.success .dot { background: var(--signal); }
.toast.error .dot { background: var(--danger); }
.toast-enter-active, .toast-leave-active { transition: all .2s ease; }
.toast-enter-from, .toast-leave-to { opacity: 0; transform: translateY(8px); }
</style>
