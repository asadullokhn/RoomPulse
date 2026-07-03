<script setup lang="ts">
import { ref } from 'vue'
import type { User, Reservation } from '@/api/types'
import { getUserReservations, deleteUser, adminCancelReservation } from '@/api/client'
import Badge from '@/components/Badge.vue'

defineProps<{ users: User[] }>()
const emit = defineEmits<{ changed: [] }>()

const expandedId = ref<string | null>(null)
const bookingsByUser = ref<Record<string, Reservation[]>>({})
const busy = ref(false)
const error = ref('')

function fmtTime(s?: string) { return s ? new Date(s).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }) : '—' }
function statusTone(s: string): 'muted' | 'danger' | 'signal' {
  return s === 'released' || s === 'cancelled' ? 'muted' : s === 'no_show' ? 'danger' : 'signal'
}

async function toggleExpand(userId: string) {
  if (expandedId.value === userId) {
    expandedId.value = null
    return
  }
  expandedId.value = userId
  error.value = ''
  try {
    bookingsByUser.value[userId] = await getUserReservations(userId)
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'failed to load bookings'
  }
}

async function cancelBooking(userId: string, reservationId: string) {
  busy.value = true
  error.value = ''
  try {
    await adminCancelReservation(reservationId)
    bookingsByUser.value[userId] = await getUserReservations(userId)
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'cancel failed'
  } finally {
    busy.value = false
  }
}

async function removeUser(userId: string) {
  if (!confirm('Delete this account? Their open bookings will be cancelled and sessions revoked.')) return
  busy.value = true
  error.value = ''
  try {
    await deleteUser(userId)
    if (expandedId.value === userId) expandedId.value = null
    emit('changed')
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'delete failed'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="card">
    <div class="scroll">
      <table>
        <thead><tr><th></th><th>Name</th><th>Email</th><th>User ID</th><th>Joined</th><th></th></tr></thead>
        <tbody>
          <template v-for="u in users" :key="u.user_id">
            <tr>
              <td class="expand-cell">
                <button class="btn-ghost" @click="toggleExpand(u.user_id)">{{ expandedId === u.user_id ? '−' : '+' }}</button>
              </td>
              <td>{{ u.name || '—' }}</td>
              <td class="mono">{{ u.email || '—' }}</td>
              <td class="mono id">{{ u.user_id }}</td>
              <td class="muted">{{ fmtTime(u.created_at) }}</td>
              <td class="actions">
                <button class="btn-ghost" :disabled="busy" @click="removeUser(u.user_id)">Delete</button>
              </td>
            </tr>
            <tr v-if="expandedId === u.user_id" class="bookings-row">
              <td colspan="6">
                <div v-if="!bookingsByUser[u.user_id]" class="empty">Loading bookings…</div>
                <div v-else-if="!bookingsByUser[u.user_id].length" class="empty">No bookings.</div>
                <table v-else class="bookings">
                  <thead><tr><th>Room</th><th>Window</th><th>Status</th><th>Source</th><th></th></tr></thead>
                  <tbody>
                    <tr v-for="r in bookingsByUser[u.user_id]" :key="r.reservation_id">
                      <td class="mono">{{ r.zoom_workspace_id }}</td>
                      <td class="mono muted">{{ fmtTime(r.start_time) }}–{{ fmtTime(r.end_time) }}</td>
                      <td><Badge :tone="statusTone(r.status)">{{ r.status }}</Badge></td>
                      <td class="muted">{{ r.source || 'zoom' }}</td>
                      <td class="actions">
                        <button
                          v-if="r.source === 'app' && r.status === 'booked'"
                          class="btn-ghost"
                          :disabled="busy"
                          @click="cancelBooking(u.user_id, r.reservation_id)"
                        >Cancel</button>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </td>
            </tr>
          </template>
          <tr v-if="!users.length"><td colspan="6" class="empty">No accounts yet.</td></tr>
        </tbody>
      </table>
    </div>
    <div v-if="error" class="err">{{ error }}</div>
  </div>
</template>

<style scoped>
.expand-cell { width: 32px; }
.actions { display: flex; gap: 6px; white-space: nowrap; }
button { font-family: var(--f-body); font-size: 12px; font-weight: 500; cursor: pointer;
  border-radius: 8px; padding: 6px 11px; border: 1px solid transparent; }
.btn-ghost { background: transparent; color: var(--text); border-color: var(--line); }
.btn-ghost:hover { border-color: var(--accent); }
.btn-ghost:disabled { opacity: .5; cursor: default; }
.muted { color: var(--muted); }
.empty { padding: 14px 4px; color: var(--faint); font-size: 12.5px; }
.err { padding: 10px 16px; color: var(--danger); font-size: 12.5px; border-top: 1px solid var(--line-soft); }
.bookings-row td { padding: 4px 12px 14px; background: rgba(150,170,220,.04); }
.bookings { width: 100%; }
.bookings th { text-align: left; font-size: 11px; text-transform: uppercase; letter-spacing: .04em; color: var(--faint); padding: 6px 8px; }
.bookings td { padding: 6px 8px; font-size: 13px; }
</style>
