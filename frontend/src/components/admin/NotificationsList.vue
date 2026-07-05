<script setup lang="ts">
import { ref } from 'vue'
import type { Notification } from '@/api/types'
import { deleteNotification, clearNotifications } from '@/api/client'

defineProps<{ notifications: Notification[] }>()
const emit = defineEmits<{ changed: [] }>()

const busy = ref(false)

function noteTone(t: string): 'danger' | 'amber' | 'muted' | 'signal' {
  return t === 'collision' ? 'danger' : t === 'overstay' ? 'amber' : t === 'no_show_released' ? 'muted' : t === 'room_freed' ? 'signal' : 'amber'
}
function noteLabel(t: string) { return (t || '').replace(/_/g, ' ') }
function fmtClock(s: string) {
  return s ? new Date(s).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }) : ''
}

async function dismiss(id: number) {
  busy.value = true
  try {
    await deleteNotification(id)
    emit('changed')
  } finally {
    busy.value = false
  }
}

async function clearAll() {
  busy.value = true
  try {
    await clearNotifications()
    emit('changed')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="notes">
    <div v-if="notifications.length" class="toolbar">
      <button class="btn-ghost" :disabled="busy" @click="clearAll()">Clear all</button>
    </div>
    <div v-for="n in notifications" :key="n.id" class="note">
      <span class="badge" :class="`b-${noteTone(n.type)}`">{{ noteLabel(n.type) }}</span>
      <div>
        <div class="nt">{{ n.title }}</div>
        <div class="nb">{{ n.body }}</div>
      </div>
      <div class="meta">
        <div class="muted" style="font-size:12px">{{ n.recipient || 'broadcast' }}</div>
        <div class="time">{{ fmtClock(n.created_at) }}</div>
      </div>
      <button class="btn-ghost dismiss" :disabled="busy" @click="dismiss(n.id)">Dismiss</button>
    </div>
    <div v-if="!notifications.length" class="empty">No notifications yet.</div>
  </div>
</template>

<style scoped>
.notes { display: grid; gap: 8px; }
.toolbar { display: flex; justify-content: flex-end; }
.note { display: flex; gap: 12px; align-items: flex-start; padding: 11px 14px; border: 1px solid var(--line);
  border-radius: 11px; background: rgba(150,170,220,.03); }
.note .nt { font-weight: 600; font-size: 13px; }
.note .nb { color: var(--muted); font-size: 12.5px; margin-top: 1px; }
.note .meta { margin-left: auto; text-align: right; flex: none; }
.note .meta .time { font-family: var(--f-mono); font-size: 10.5px; color: var(--faint); }
.btn-ghost { background: none; border: 1px solid var(--line); border-radius: 8px; color: var(--muted);
  padding: 4px 10px; font-size: 11.5px; cursor: pointer; font-family: var(--f-body); flex: none; }
.btn-ghost:hover { color: var(--ink); border-color: var(--signal-line); }
.dismiss { align-self: center; }
</style>
