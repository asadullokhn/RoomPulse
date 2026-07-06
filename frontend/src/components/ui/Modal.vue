<script setup lang="ts">
import { watch } from 'vue'

const props = defineProps<{
  title: string
  open: boolean
  variant?: 'form' | 'confirm'
  confirmLabel?: string
  danger?: boolean
  busy?: boolean
  wide?: boolean
}>()
const emit = defineEmits<{ close: []; confirm: [] }>()

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}
watch(() => props.open, (open) => {
  if (open) window.addEventListener('keydown', onKey)
  else window.removeEventListener('keydown', onKey)
})
</script>

<template>
  <Teleport to="body">
    <Transition name="modal">
      <div v-if="open" class="backdrop" @mousedown.self="emit('close')">
        <div class="panel" :class="{ wide }" role="dialog" aria-modal="true" :aria-label="title">
          <header>
            <h2>{{ title }}</h2>
            <button class="x" aria-label="Close" @click="emit('close')">&#215;</button>
          </header>
          <div class="body">
            <slot />
          </div>
          <footer v-if="variant === 'confirm'">
            <button class="btn-secondary" @click="emit('close')">Cancel</button>
            <button :class="danger ? 'btn-confirm-danger' : 'btn-primary'" :disabled="busy" @click="emit('confirm')">
              {{ confirmLabel ?? 'Confirm' }}
            </button>
          </footer>
          <footer v-else-if="$slots.footer">
            <slot name="footer" />
          </footer>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.backdrop { position: fixed; inset: 0; z-index: 100; background: rgba(0, 0, 0, .30);
  backdrop-filter: blur(2px); -webkit-backdrop-filter: blur(2px);
  display: grid; place-items: center; padding: 20px; }
.panel { width: min(440px, 100%); background: var(--panel); border-radius: 14px;
  box-shadow: var(--shadow-pop); display: flex; flex-direction: column; max-height: 90vh; }
.panel.wide { width: min(720px, 100%); }
header { display: flex; align-items: center; justify-content: space-between; padding: 16px 20px 0; }
h2 { margin: 0; font-family: var(--f-display); font-size: 17px; font-weight: 700; letter-spacing: -0.01em; }
.x { background: var(--page); border: none; border-radius: 50%; width: 26px; height: 26px;
  color: var(--muted); font-size: 15px; line-height: 1; cursor: pointer; }
.x:hover { color: var(--text); }
.body { padding: 14px 20px 20px; overflow-y: auto; }
footer { display: flex; justify-content: flex-end; gap: 8px; padding: 0 20px 18px; }
.btn-confirm-danger { background: var(--danger); color: #fff; border: none; border-radius: 980px;
  padding: 8px 18px; font-size: 13px; font-weight: 600; font-family: var(--f-body); cursor: pointer; }
.btn-confirm-danger:hover { background: #e0342a; }
.btn-confirm-danger:disabled { opacity: .45; cursor: default; }
.modal-enter-active, .modal-leave-active { transition: opacity .18s ease; }
.modal-enter-active .panel, .modal-leave-active .panel { transition: transform .18s ease; }
.modal-enter-from, .modal-leave-to { opacity: 0; }
.modal-enter-from .panel, .modal-leave-to .panel { transform: scale(.98); }
</style>
