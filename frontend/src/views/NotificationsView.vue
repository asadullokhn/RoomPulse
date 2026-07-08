<script setup lang="ts">
import { ref, computed } from 'vue'
import { usePoll } from '@/composables/usePoll'
import { getNotifications, deleteNotification, clearNotifications } from '@/api/client'
import { useToast } from '@/composables/useToast'
import type { Notification } from '@/api/types'
import Toolbar from '@/components/ui/Toolbar.vue'
import SegmentedControl from '@/components/ui/SegmentedControl.vue'
import Pagination from '@/components/ui/Pagination.vue'
import Modal from '@/components/ui/Modal.vue'

document.title = 'QuickRoom · Notifications'

const toast = useToast()
const notifications = ref<Notification[]>([])
const loaded = ref(false)
const search = ref('')
const typeFilter = ref('all')
const page = ref(1)
const PER_PAGE = 25
const busy = ref(false)
const clearOpen = ref(false)

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  return notifications.value.filter(n => {
    if (typeFilter.value !== 'all' && n.type !== typeFilter.value) return false
    if (!q) return true
    return `${n.recipient} ${n.title} ${n.body}`.toLowerCase().includes(q)
  })
})
const paged = computed(() => filtered.value.slice((page.value - 1) * PER_PAGE, page.value * PER_PAGE))

function tone(t: string) {
  return t === 'collision' ? 'b-danger' : t === 'overstay' ? 'b-amber' : 'b-muted'
}
function label(t: string) { return (t || '').replace(/_/g, ' ') }
function fmtClock(s: string) {
  return s ? new Date(s).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }) : ''
}

async function dismiss(id: number) {
  busy.value = true
  try {
    await deleteNotification(id)
    await refresh()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : 'dismiss failed')
  } finally {
    busy.value = false
  }
}

async function confirmClear() {
  busy.value = true
  try {
    await clearNotifications()
    toast.success('Outbox cleared')
    clearOpen.value = false
    await refresh()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : 'clear failed')
  } finally {
    busy.value = false
  }
}

async function refresh() {
  notifications.value = await getNotifications(200)
  loaded.value = true
}
usePoll(() => refresh().catch(() => {}), 4000)
</script>

<template>
  <div>
    <header class="vh">
      <div>
        <h1>Notifications</h1>
        <p class="sub">Everything pushed to phones: reminders, releases, and freed rooms.</p>
      </div>
      <div class="vh-actions">
        <button class="btn-secondary" :disabled="!notifications.length" @click="clearOpen = true">Clear all</button>
      </div>
    </header>

    <div v-if="!loaded" class="empty">Loading&#8230;</div>
    <template v-else>
      <Toolbar v-model:search="search" search-placeholder="Search recipient or text" @update:search="page = 1">
        <template #filters>
          <SegmentedControl
            v-model="typeFilter"
            :options="[
              { value: 'all', label: 'All' },
              { value: 'grace_reminder', label: 'Reminders' },
              { value: 'no_show_released', label: 'Releases' },
              { value: 'collision', label: 'Conflicts' },
              { value: 'overstay', label: 'Overstays' },
            ]"
            @update:model-value="page = 1"
          />
        </template>
      </Toolbar>

      <div class="card">
        <div v-if="!paged.length" class="empty"><b>Nothing here.</b>Notifications appear as the day unfolds.</div>
        <div v-else class="notes">
          <div v-for="n in paged" :key="n.id" class="note">
            <span class="badge" :class="tone(n.type)">{{ label(n.type) }}</span>
            <div class="txt">
              <div class="nt">{{ n.title }}</div>
              <div class="nb">{{ n.body }}</div>
            </div>
            <div class="meta">
              <div class="to">{{ n.recipient || 'broadcast' }}</div>
              <div class="time">{{ fmtClock(n.created_at) }}</div>
            </div>
            <button class="btn-ghost" :disabled="busy" @click="dismiss(n.id)">Dismiss</button>
          </div>
        </div>
      </div>
      <Pagination v-model:page="page" :total="filtered.length" :per-page="PER_PAGE" />
    </template>

    <Modal
      title="Clear the outbox?"
      :open="clearOpen"
      variant="confirm"
      confirm-label="Clear all"
      danger
      :busy="busy"
      @close="clearOpen = false"
      @confirm="confirmClear"
    >
      <p class="confirm-text">All {{ notifications.length }} entries disappear from this list. Phones keep what was already delivered.</p>
    </Modal>
  </div>
</template>

<style scoped>
.notes { display: grid; }
.note { display: flex; align-items: center; gap: 12px; padding: 11px 16px; border-bottom: 1px solid var(--line-soft); }
.note:last-child { border-bottom: 0; }
.note .badge { flex: none; }
.txt { min-width: 0; }
.nt { font-size: 13px; font-weight: 600; }
.nb { font-size: 12.5px; color: var(--muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.meta { margin-left: auto; text-align: right; flex: none; }
.to { font-size: 12px; color: var(--muted); }
.time { font-size: 10.5px; color: var(--faint); font-variant-numeric: tabular-nums; }
.confirm-text { margin: 0; font-size: 13px; color: var(--muted); }
</style>
