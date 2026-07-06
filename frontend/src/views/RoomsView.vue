<script setup lang="ts">
import { ref, computed } from 'vue'
import { usePoll } from '@/composables/usePoll'
import { getRooms, getOccupancy, getBeacons, getEvents, getReservations, createRoom, patchRoom, deleteRoom } from '@/api/client'
import { useToast } from '@/composables/useToast'
import type { Room, OccupancyEntry, Beacon, EventEntry, Reservation } from '@/api/types'
import Toolbar from '@/components/ui/Toolbar.vue'
import Modal from '@/components/ui/Modal.vue'
import SegmentedControl from '@/components/ui/SegmentedControl.vue'

document.title = 'QuickRoom · Rooms'

const toast = useToast()
const rooms = ref<Room[]>([])
const occupancy = ref<OccupancyEntry[]>([])
const beacons = ref<Beacon[]>([])
const loaded = ref(false)
const search = ref('')

// full-history modal
const historyRoom = ref<Room | null>(null)
const historyTab = ref('activity')
const historyEvents = ref<EventEntry[]>([])
const historyBookings = ref<Reservation[]>([])
const historyLoading = ref(false)

const formOpen = ref(false)
const editingWs = ref('')
const formName = ref('')
const formCapacity = ref(4)
const formError = ref('')
const busy = ref(false)
const deleteTarget = ref<Room | null>(null)

const occByWs = computed(() => {
  const m: Record<string, number> = {}
  for (const o of occupancy.value) m[o.workspace_id] = o.count
  return m
})
const beaconByWs = computed(() => {
  const m: Record<string, Beacon> = {}
  for (const b of beacons.value) m[b.workspace_id] = b
  return m
})
const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  return q ? rooms.value.filter(r => r.name.toLowerCase().includes(q)) : rooms.value
})

function isCustom(ws: string) { return ws.startsWith('cr-') }

const HISTORY_STATUS: Record<string, { label: string; tone: string }> = {
  booked: { label: 'Booked', tone: 'b-blue' },
  no_show: { label: 'No-show', tone: 'b-amber' },
  released: { label: 'Released', tone: 'b-muted' },
  cancelled: { label: 'Cancelled', tone: 'b-muted' },
}
function bookingBadge(r: Reservation) {
  if (r.status === 'booked' && r.check_in_status === 'checked_in') return { label: 'Checked in', tone: 'b-signal' }
  if (r.status === 'booked' && r.check_in_status === 'checked_out') return { label: 'Checked out', tone: 'b-muted' }
  return HISTORY_STATUS[r.status] ?? HISTORY_STATUS.booked
}
function agoLabel(sec: number) {
  if (sec < 60) return `${sec}s ago`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ${Math.floor((sec % 3600) / 60)}m ago`
  return `${Math.floor(sec / 86400)}d ago`
}
function fmtWindow(r: Reservation) {
  const f = (s: string) => new Date(s).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  return `${f(r.start_time)} – ${new Date(r.end_time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`
}

async function openHistory(r: Room) {
  historyRoom.value = r
  historyTab.value = 'activity'
  historyLoading.value = true
  historyEvents.value = []
  historyBookings.value = []
  try {
    const [events, all] = await Promise.all([getEvents(r.zoom_workspace_id, 200), getReservations()])
    historyEvents.value = events
    historyBookings.value = all
      .filter(b => b.zoom_workspace_id === r.zoom_workspace_id)
      .sort((a, b) => new Date(b.start_time).getTime() - new Date(a.start_time).getTime())
  } catch (e) {
    toast.error(e instanceof Error ? e.message : 'could not load history')
  } finally {
    historyLoading.value = false
  }
}

function openAdd() {
  editingWs.value = ''
  formName.value = ''
  formCapacity.value = 4
  formError.value = ''
  formOpen.value = true
}
function openEdit(r: Room) {
  editingWs.value = r.zoom_workspace_id
  formName.value = r.name
  formCapacity.value = r.capacity
  formError.value = ''
  formOpen.value = true
}

async function submitForm() {
  busy.value = true
  formError.value = ''
  try {
    if (editingWs.value) {
      await patchRoom(editingWs.value, { name: formName.value, capacity: formCapacity.value })
      toast.success('Room updated')
    } else {
      await createRoom({ name: formName.value, capacity: formCapacity.value, has_tv: false })
      toast.success('Room added')
    }
    formOpen.value = false
    await refresh()
  } catch (e) {
    formError.value = e instanceof Error ? e.message : 'request failed'
  } finally {
    busy.value = false
  }
}

async function confirmDelete() {
  if (!deleteTarget.value) return
  busy.value = true
  const custom = isCustom(deleteTarget.value.zoom_workspace_id)
  try {
    await deleteRoom(deleteTarget.value.zoom_workspace_id)
    toast.success(custom ? 'Room deleted' : 'Room reset to Zoom values')
    deleteTarget.value = null
    await refresh()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : 'request failed')
  } finally {
    busy.value = false
  }
}

async function refresh() {
  const [r, occ, b] = await Promise.all([getRooms(), getOccupancy(), getBeacons()])
  rooms.value = r; occupancy.value = occ; beacons.value = b
  loaded.value = true
}
usePoll(() => refresh().catch(() => {}), 4000)
</script>

<template>
  <div>
    <header class="vh">
      <div>
        <h1>Rooms</h1>
        <p class="sub">Zoom-synced rooms stay true to Zoom; your edits become overrides that survive every sync.</p>
      </div>
      <div class="vh-actions">
        <button class="btn-primary" @click="openAdd()">Add room</button>
      </div>
    </header>

    <div v-if="!loaded" class="empty">Loading&#8230;</div>
    <template v-else>
      <Toolbar v-model:search="search" search-placeholder="Search rooms" />
      <div class="card scroll">
        <table>
          <thead>
            <tr><th>Name</th><th>Capacity</th><th>Type</th><th>Present</th><th>Beacon</th><th></th></tr>
          </thead>
          <tbody>
            <tr v-for="r in filtered" :key="r.zoom_workspace_id">
              <td class="strong">{{ r.name }}</td>
              <td class="num">{{ r.capacity }}</td>
              <td>
                <span class="badge" :class="isCustom(r.zoom_workspace_id) ? 'b-blue' : 'b-muted'">
                  {{ isCustom(r.zoom_workspace_id) ? 'Custom' : 'Zoom' }}
                </span>
              </td>
              <td>
                <span class="occ" :class="{ on: (occByWs[r.zoom_workspace_id] ?? 0) > 0 }">
                  {{ occByWs[r.zoom_workspace_id] ?? 0 }}
                </span>
              </td>
              <td class="mono">
                {{ beaconByWs[r.zoom_workspace_id] ? `${beaconByWs[r.zoom_workspace_id].major} / ${beaconByWs[r.zoom_workspace_id].minor}` : '—' }}
              </td>
              <td class="actions">
                <button class="btn-ghost" @click="openHistory(r)">History</button>
                <button class="btn-ghost" @click="openEdit(r)">Edit</button>
                <button class="btn-danger-ghost" @click="deleteTarget = r">
                  {{ isCustom(r.zoom_workspace_id) ? 'Delete' : 'Reset' }}
                </button>
              </td>
            </tr>
            <tr v-if="!filtered.length">
              <td colspan="6" class="empty"><b>No rooms match.</b>Try a different search.</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <Modal :title="historyRoom ? `${historyRoom.name} — history` : ''" :open="historyRoom !== null" @close="historyRoom = null">
      <template v-if="historyRoom">
        <SegmentedControl
          v-model="historyTab"
          :options="[
            { value: 'activity', label: `Activity (${historyEvents.length})` },
            { value: 'bookings', label: `Bookings (${historyBookings.length})` },
          ]"
        />
        <div v-if="historyLoading" class="empty">Loading&#8230;</div>
        <template v-else-if="historyTab === 'activity'">
          <ul class="hist">
            <li v-for="(e, i) in historyEvents" :key="i">
              <span class="badge" :class="e.kind === 'enter' ? 'b-signal' : 'b-muted'">{{ e.kind === 'enter' ? 'entered' : 'left' }}</span>
              <span class="who">{{ e.name || e.actor }}</span>
              <span class="when">{{ agoLabel(e.ago_sec) }}</span>
            </li>
            <li v-if="!historyEvents.length" class="none">No presence activity recorded yet.</li>
          </ul>
        </template>
        <template v-else>
          <ul class="hist">
            <li v-for="b in historyBookings" :key="b.reservation_id">
              <span class="badge" :class="bookingBadge(b).tone">{{ bookingBadge(b).label }}</span>
              <span class="who">{{ b.user_email || b.user_id || 'unknown' }}</span>
              <span class="when">{{ fmtWindow(b) }}</span>
            </li>
            <li v-if="!historyBookings.length" class="none">No bookings for this room yet.</li>
          </ul>
        </template>
      </template>
    </Modal>

    <Modal :title="editingWs ? 'Edit room' : 'Add room'" :open="formOpen" @close="formOpen = false">
      <form class="form" @submit.prevent="submitForm">
        <label><span>Name</span><input v-model.trim="formName" class="field" required /></label>
        <label><span>Capacity</span><input v-model.number="formCapacity" class="field" type="number" min="0" required /></label>
        <p v-if="editingWs && !isCustom(editingWs)" class="hint">
          This is a Zoom-synced room: your change is stored as an override and re-applied after every sync.
        </p>
        <div v-if="formError" class="ferr">{{ formError }}</div>
        <div class="factions">
          <button type="button" class="btn-secondary" @click="formOpen = false">Cancel</button>
          <button type="submit" class="btn-primary" :disabled="busy || !formName">
            {{ editingWs ? 'Save changes' : 'Add room' }}
          </button>
        </div>
      </form>
    </Modal>

    <Modal
      :title="deleteTarget && isCustom(deleteTarget.zoom_workspace_id) ? 'Delete this room?' : 'Reset to Zoom values?'"
      :open="deleteTarget !== null"
      variant="confirm"
      :confirm-label="deleteTarget && isCustom(deleteTarget.zoom_workspace_id) ? 'Delete room' : 'Reset room'"
      :danger="!!deleteTarget && isCustom(deleteTarget.zoom_workspace_id)"
      :busy="busy"
      @close="deleteTarget = null"
      @confirm="confirmDelete"
    >
      <p class="confirm-text" v-if="deleteTarget">
        <template v-if="isCustom(deleteTarget.zoom_workspace_id)">
          {{ deleteTarget.name }} disappears from the floor and its open bookings are cancelled.
        </template>
        <template v-else>
          {{ deleteTarget.name }} drops your edits and returns to its Zoom name and capacity on the next sync.
        </template>
      </p>
    </Modal>
  </div>
</template>

<style scoped>
.strong { font-weight: 600; }
.actions { text-align: right; white-space: nowrap; }
.occ { font-variant-numeric: tabular-nums; color: var(--muted); }
.occ.on { color: #1d8a3e; font-weight: 600; }
.form { display: grid; gap: 12px; }
.form label { display: grid; gap: 5px; font-size: 12px; color: var(--muted); font-weight: 500; }
.hint { margin: 0; font-size: 12px; color: var(--faint); }
.ferr { color: var(--danger); font-size: 12.5px; }
.factions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 4px; }
.confirm-text { margin: 0; font-size: 13px; color: var(--muted); }
.hist { list-style: none; margin: 14px 0 0; padding: 0; max-height: 420px; overflow-y: auto; display: grid; gap: 2px; }
.hist li { display: flex; align-items: center; gap: 10px; padding: 7px 4px; font-size: 13px;
  border-bottom: 1px solid var(--line-soft); }
.hist li:last-child { border-bottom: none; }
.hist .who { font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.hist .when { margin-left: auto; color: var(--muted); font-size: 12px; white-space: nowrap; font-variant-numeric: tabular-nums; }
.hist .none { color: var(--muted); justify-content: center; padding: 18px 0; }
</style>
