<script setup lang="ts">
import { ref, computed } from 'vue'
import { usePoll } from '@/composables/usePoll'
import { getUsers, getUserReservations, deleteUser, adminCancelReservation, renameUser } from '@/api/client'
import { useToast } from '@/composables/useToast'
import type { User, Reservation } from '@/api/types'
import Toolbar from '@/components/ui/Toolbar.vue'
import Pagination from '@/components/ui/Pagination.vue'
import Modal from '@/components/ui/Modal.vue'

document.title = 'QuickRoom · Users'

const toast = useToast()
const users = ref<User[]>([])
const loaded = ref(false)
const search = ref('')
const page = ref(1)
const PER_PAGE = 25
const busy = ref(false)

const expandedId = ref<string | null>(null)
const bookingsByUser = ref<Record<string, Reservation[]>>({})
const renamingId = ref<string | null>(null)
const renameValue = ref('')
const deleteTarget = ref<User | null>(null)

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return users.value
  return users.value.filter(u => `${u.name} ${u.email} ${u.user_id}`.toLowerCase().includes(q))
})
const paged = computed(() => filtered.value.slice((page.value - 1) * PER_PAGE, page.value * PER_PAGE))

function fmtJoined(s?: string) {
  return s ? new Date(s).toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' }) : '—'
}
function fmtWindow(r: Reservation) {
  const f = (s: string) => new Date(s).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  return `${f(r.start_time)} – ${new Date(r.end_time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`
}
function statusTone(s: string) { return s === 'booked' ? 'b-blue' : s === 'no_show' ? 'b-danger' : 'b-muted' }

async function toggleExpand(userId: string) {
  if (expandedId.value === userId) { expandedId.value = null; return }
  expandedId.value = userId
  try {
    bookingsByUser.value[userId] = await getUserReservations(userId)
  } catch (e) {
    toast.error(e instanceof Error ? e.message : 'failed to load bookings')
  }
}

async function cancelBooking(userId: string, reservationId: string) {
  busy.value = true
  try {
    await adminCancelReservation(reservationId)
    toast.success('Booking cancelled')
    bookingsByUser.value[userId] = await getUserReservations(userId)
  } catch (e) {
    toast.error(e instanceof Error ? e.message : 'cancel failed')
  } finally {
    busy.value = false
  }
}

function startRename(u: User) {
  renamingId.value = u.user_id
  renameValue.value = u.name || ''
}

async function saveRename(userId: string) {
  busy.value = true
  try {
    await renameUser(userId, renameValue.value)
    toast.success('Name updated')
    renamingId.value = null
    await refresh()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : 'rename failed')
  } finally {
    busy.value = false
  }
}

async function confirmDelete() {
  if (!deleteTarget.value) return
  busy.value = true
  try {
    await deleteUser(deleteTarget.value.user_id)
    toast.success('Account deleted')
    if (expandedId.value === deleteTarget.value.user_id) expandedId.value = null
    deleteTarget.value = null
    await refresh()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : 'delete failed')
  } finally {
    busy.value = false
  }
}

async function refresh() {
  users.value = await getUsers()
  loaded.value = true
}
usePoll(() => refresh().catch(() => {}), 4000)
</script>

<template>
  <div>
    <header class="vh">
      <div>
        <h1>Users</h1>
        <p class="sub">{{ users.length }} accounts signed in with Apple.</p>
      </div>
    </header>

    <div v-if="!loaded" class="empty">Loading&#8230;</div>
    <template v-else>
      <Toolbar v-model:search="search" search-placeholder="Search name or email" @update:search="page = 1" />
      <div class="card scroll">
        <table>
          <thead>
            <tr><th></th><th>Name</th><th>Email</th><th>User ID</th><th>Joined</th><th></th></tr>
          </thead>
          <tbody>
            <template v-for="u in paged" :key="u.user_id">
              <tr>
                <td class="expand">
                  <button class="chev" :class="{ open: expandedId === u.user_id }" :aria-label="expandedId === u.user_id ? 'Collapse' : 'Expand'" @click="toggleExpand(u.user_id)" />
                </td>
                <td class="strong">
                  <template v-if="renamingId === u.user_id">
                    <input v-model.trim="renameValue" class="field rename" @keyup.enter="saveRename(u.user_id)" />
                  </template>
                  <template v-else>{{ u.name || '—' }}</template>
                </td>
                <td class="mutedc">{{ u.email || '—' }}</td>
                <td class="mono">{{ u.user_id }}</td>
                <td class="mutedc">{{ fmtJoined(u.created_at) }}</td>
                <td class="actions">
                  <template v-if="renamingId === u.user_id">
                    <button class="btn-ghost" :disabled="busy || !renameValue" @click="saveRename(u.user_id)">Save</button>
                    <button class="btn-ghost" @click="renamingId = null">Cancel</button>
                  </template>
                  <template v-else>
                    <button class="btn-ghost" @click="startRename(u)">Rename</button>
                    <button class="btn-danger-ghost" :disabled="busy" @click="deleteTarget = u">Delete</button>
                  </template>
                </td>
              </tr>
              <tr v-if="expandedId === u.user_id" class="expand-row">
                <td colspan="6">
                  <div v-if="!bookingsByUser[u.user_id]" class="empty">Loading bookings&#8230;</div>
                  <div v-else-if="!bookingsByUser[u.user_id].length" class="empty">No bookings yet.</div>
                  <table v-else class="inner">
                    <thead><tr><th>Room</th><th>Window</th><th>Status</th><th>Source</th><th></th></tr></thead>
                    <tbody>
                      <tr v-for="r in bookingsByUser[u.user_id]" :key="r.reservation_id">
                        <td class="mono">{{ r.zoom_workspace_id }}</td>
                        <td class="mutedc">{{ fmtWindow(r) }}</td>
                        <td><span class="badge" :class="statusTone(r.status)">{{ r.status.replace('_', ' ') }}</span></td>
                        <td class="mutedc">{{ r.source || 'zoom' }}</td>
                        <td class="actions">
                          <button
                            v-if="r.source === 'app' && r.status === 'booked'"
                            class="btn-danger-ghost"
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
            <tr v-if="!paged.length">
              <td colspan="6" class="empty"><b>No accounts match.</b>Try a different search.</td>
            </tr>
          </tbody>
        </table>
      </div>
      <Pagination v-model:page="page" :total="filtered.length" :per-page="PER_PAGE" />
    </template>

    <Modal
      title="Delete this account?"
      :open="deleteTarget !== null"
      variant="confirm"
      confirm-label="Delete account"
      danger
      :busy="busy"
      @close="deleteTarget = null"
      @confirm="confirmDelete"
    >
      <p class="confirm-text" v-if="deleteTarget">
        {{ deleteTarget.name || deleteTarget.email || deleteTarget.user_id }} loses access immediately;
        their open bookings are cancelled.
      </p>
    </Modal>
  </div>
</template>

<style scoped>
.strong { font-weight: 600; }
.mutedc { color: var(--muted); }
.actions { text-align: right; white-space: nowrap; }
.expand { width: 34px; }
.chev { width: 20px; height: 20px; border: none; background: none; cursor: pointer; position: relative; }
.chev::before { content: ""; position: absolute; left: 6px; top: 6px; width: 7px; height: 7px;
  border-right: 1.6px solid var(--muted); border-bottom: 1.6px solid var(--muted);
  transform: rotate(-45deg); transition: transform .16s ease; }
.chev.open::before { transform: rotate(45deg); }
.rename { width: 150px; padding: 5px 8px; }
.expand-row td { background: var(--raised); padding-top: 4px; }
.inner { min-width: 0; }
.inner th { background: transparent; position: static; }
.confirm-text { margin: 0; font-size: 13px; color: var(--muted); }
</style>
