<script setup lang="ts">
import { ref, computed } from 'vue'
import { usePoll } from '@/composables/usePoll'
import { getReservations, getRooms, adminCreateReservation, adminPatchReservation, adminCancelReservation } from '@/api/client'
import { useToast } from '@/composables/useToast'
import type { Reservation, Room } from '@/api/types'
import ScheduleGrid from '@/components/schedule/ScheduleGrid.vue'
import SegmentedControl from '@/components/ui/SegmentedControl.vue'
import Toolbar from '@/components/ui/Toolbar.vue'
import Pagination from '@/components/ui/Pagination.vue'
import Modal from '@/components/ui/Modal.vue'

document.title = 'QuickRoom · Reservations'

const toast = useToast()
const rooms = ref<Room[]>([])
const reservations = ref<Reservation[]>([])
const loaded = ref(false)

const tab = ref('schedule')
const date = ref(new Date())

// list state
const search = ref('')
const statusFilter = ref('all')
const sourceFilter = ref('all')
const page = ref(1)
const PER_PAGE = 25

// modal state
const detail = ref<Reservation | null>(null)
const formOpen = ref(false)
const editingId = ref('')
const formWs = ref('')
const formStart = ref('')
const formEnd = ref('')
const formEmail = ref('')
const formError = ref('')
const busy = ref(false)
const cancelTarget = ref<Reservation | null>(null)

const dateLabel = computed(() =>
  date.value.toLocaleDateString([], { weekday: 'long', month: 'long', day: 'numeric' }))

const dateInput = computed({
  get: () => toLocalDate(date.value),
  set: (v: string) => { if (v) date.value = new Date(`${v}T12:00:00`) },
})

function stepDay(delta: number) {
  const d = new Date(date.value)
  d.setDate(d.getDate() + delta)
  date.value = d
}

function roomName(ws: string) { return rooms.value.find(r => r.zoom_workspace_id === ws)?.name ?? ws }
function fmtWindow(r: Reservation) {
  const f = (s: string) => new Date(s).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  return `${f(r.start_time)} – ${new Date(r.end_time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`
}
function statusTone(s: string) { return s === 'booked' ? 'b-blue' : s === 'no_show' ? 'b-danger' : 'b-muted' }
function checkTone(s: string) { return s === 'checked_in' ? 'b-signal' : 'b-muted' }
function checkLabel(s: string) { return s === 'checked_in' ? 'checked in' : s === 'checked_out' ? 'checked out' : 'awaiting' }
function editable(r: Reservation) { return r.source === 'app' && r.status === 'booked' }

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  return reservations.value.filter(r => {
    if (statusFilter.value !== 'all' && r.status !== statusFilter.value) return false
    if (sourceFilter.value !== 'all' && (r.source || 'zoom') !== sourceFilter.value) return false
    if (!q) return true
    const hay = `${r.user_email} ${r.user_id} ${r.booked_by_user_id ?? ''} ${roomName(r.zoom_workspace_id)}`.toLowerCase()
    return hay.includes(q)
  })
})
const paged = computed(() => filtered.value.slice((page.value - 1) * PER_PAGE, page.value * PER_PAGE))

function toLocalDate(d: Date) {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}
function toLocalInput(d: Date) {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${toLocalDate(d)}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function openNew(slot?: { workspaceId: string; start: Date; end: Date }) {
  editingId.value = ''
  formWs.value = slot?.workspaceId ?? ''
  formStart.value = slot ? toLocalInput(slot.start) : ''
  formEnd.value = slot ? toLocalInput(slot.end) : ''
  formEmail.value = ''
  formError.value = ''
  formOpen.value = true
}

function openEdit(r: Reservation) {
  detail.value = null
  editingId.value = r.reservation_id
  formWs.value = r.zoom_workspace_id
  formStart.value = toLocalInput(new Date(r.start_time))
  formEnd.value = toLocalInput(new Date(r.end_time))
  formEmail.value = r.user_email || ''
  formError.value = ''
  formOpen.value = true
}

async function submitForm() {
  busy.value = true
  formError.value = ''
  try {
    if (editingId.value) {
      await adminPatchReservation(editingId.value, {
        start_time: new Date(formStart.value).toISOString(),
        end_time: new Date(formEnd.value).toISOString(),
      })
      toast.success('Booking updated')
    } else {
      await adminCreateReservation({
        workspace_id: formWs.value,
        start_time: new Date(formStart.value).toISOString(),
        end_time: new Date(formEnd.value).toISOString(),
        user_email: formEmail.value || undefined,
      })
      toast.success('Room booked')
    }
    formOpen.value = false
    await refresh()
  } catch (e) {
    formError.value = e instanceof Error ? e.message : 'request failed'
  } finally {
    busy.value = false
  }
}

async function confirmCancel() {
  if (!cancelTarget.value) return
  busy.value = true
  try {
    await adminCancelReservation(cancelTarget.value.reservation_id)
    toast.success('Booking cancelled')
    cancelTarget.value = null
    detail.value = null
    await refresh()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : 'cancel failed')
  } finally {
    busy.value = false
  }
}

async function refresh() {
  const [r, res] = await Promise.all([getRooms(), getReservations()])
  rooms.value = r
  reservations.value = res
  loaded.value = true
}
usePoll(() => refresh().catch(() => {}), 4000)
</script>

<template>
  <div>
    <header class="vh">
      <div>
        <h1>Reservations</h1>
        <p class="sub">Every booking across {{ rooms.length }} rooms, 07:00–19:00.</p>
      </div>
      <div class="vh-actions">
        <SegmentedControl v-model="tab" :options="[{ value: 'schedule', label: 'Schedule' }, { value: 'list', label: 'List' }]" />
        <button class="btn-primary" @click="openNew()">New booking</button>
      </div>
    </header>

    <div v-if="!loaded" class="empty">Loading&#8230;</div>

    <template v-else-if="tab === 'schedule'">
      <div class="datebar">
        <button class="btn-secondary" aria-label="Previous day" @click="stepDay(-1)">&#8249;</button>
        <input v-model="dateInput" class="field" type="date" />
        <button class="btn-secondary" aria-label="Next day" @click="stepDay(1)">&#8250;</button>
        <span class="datelabel">{{ dateLabel }}</span>
        <button class="btn-ghost" @click="date = new Date()">Today</button>
      </div>
      <ScheduleGrid
        :rooms="rooms"
        :reservations="reservations"
        :date="date"
        @select="detail = $event"
        @create="openNew($event)"
      />
    </template>

    <template v-else>
      <Toolbar v-model:search="search" search-placeholder="Search booker or room">
        <template #filters>
          <SegmentedControl
            v-model="statusFilter"
            :options="[
              { value: 'all', label: 'All' },
              { value: 'booked', label: 'Booked' },
              { value: 'released', label: 'Released' },
              { value: 'cancelled', label: 'Cancelled' },
              { value: 'no_show', label: 'No-show' },
            ]"
            @update:model-value="page = 1"
          />
          <SegmentedControl
            v-model="sourceFilter"
            :options="[{ value: 'all', label: 'Any source' }, { value: 'app', label: 'App' }, { value: 'zoom', label: 'Zoom' }]"
            @update:model-value="page = 1"
          />
        </template>
      </Toolbar>

      <div class="card scroll">
        <table>
          <thead>
            <tr><th>Room</th><th>Booker</th><th>Window</th><th>Status</th><th>Check-in</th><th></th></tr>
          </thead>
          <tbody>
            <tr v-for="r in paged" :key="r.reservation_id">
              <td class="strong">{{ roomName(r.zoom_workspace_id) }}</td>
              <td class="mutedc">{{ r.user_email || r.user_id || '—' }}</td>
              <td class="num mutedc">{{ fmtWindow(r) }}</td>
              <td><span class="badge" :class="statusTone(r.status)">{{ r.status.replace('_', ' ') }}</span></td>
              <td><span class="badge" :class="checkTone(r.check_in_status)">{{ checkLabel(r.check_in_status) }}</span></td>
              <td class="actions">
                <template v-if="editable(r)">
                  <button class="btn-ghost" @click="openEdit(r)">Edit</button>
                  <button class="btn-danger-ghost" @click="cancelTarget = r">Cancel</button>
                </template>
              </td>
            </tr>
            <tr v-if="!paged.length">
              <td colspan="6" class="empty"><b>No reservations match.</b>Adjust the filters or the search.</td>
            </tr>
          </tbody>
        </table>
      </div>
      <Pagination v-model:page="page" :total="filtered.length" :per-page="PER_PAGE" />
    </template>

    <Modal :title="roomName(detail?.zoom_workspace_id ?? '')" :open="detail !== null" @close="detail = null">
      <template v-if="detail">
        <div class="dl">
          <div><span>Booker</span><b>{{ detail.user_email || detail.user_id || 'unknown' }}</b></div>
          <div><span>Window</span><b>{{ fmtWindow(detail) }}</b></div>
          <div><span>Status</span><span class="badge" :class="statusTone(detail.status)">{{ detail.status.replace('_', ' ') }}</span></div>
          <div><span>Check-in</span><span class="badge" :class="checkTone(detail.check_in_status)">{{ checkLabel(detail.check_in_status) }}</span></div>
          <div><span>Source</span><b>{{ detail.source || 'zoom' }}</b></div>
        </div>
        <div v-if="editable(detail)" class="detail-actions">
          <button class="btn-secondary" @click="openEdit(detail)">Edit booking</button>
          <button class="btn-danger-ghost" @click="cancelTarget = detail">Cancel booking</button>
        </div>
      </template>
    </Modal>

    <Modal :title="editingId ? 'Edit booking' : 'New booking'" :open="formOpen" @close="formOpen = false">
      <form class="form" @submit.prevent="submitForm">
        <label>
          <span>Room</span>
          <select v-model="formWs" class="field" :disabled="!!editingId" required>
            <option value="" disabled>Choose a room</option>
            <option v-for="rm in rooms" :key="rm.zoom_workspace_id" :value="rm.zoom_workspace_id">{{ rm.name }}</option>
          </select>
        </label>
        <div class="two">
          <label><span>Starts</span><input v-model="formStart" class="field" type="datetime-local" required /></label>
          <label><span>Ends</span><input v-model="formEnd" class="field" type="datetime-local" required /></label>
        </div>
        <label v-if="!editingId">
          <span>Booker email (optional)</span>
          <input v-model.trim="formEmail" class="field" type="email" placeholder="Used for their notifications" />
        </label>
        <div v-if="formError" class="ferr">{{ formError }}</div>
        <div class="factions">
          <button type="button" class="btn-secondary" @click="formOpen = false">Cancel</button>
          <button type="submit" class="btn-primary" :disabled="busy || !formWs || !formStart || !formEnd">
            {{ editingId ? 'Save changes' : 'Book room' }}
          </button>
        </div>
      </form>
    </Modal>

    <Modal
      title="Cancel this booking?"
      :open="cancelTarget !== null"
      variant="confirm"
      confirm-label="Cancel booking"
      danger
      :busy="busy"
      @close="cancelTarget = null"
      @confirm="confirmCancel"
    >
      <p class="confirm-text" v-if="cancelTarget">
        {{ roomName(cancelTarget.zoom_workspace_id) }}, {{ fmtWindow(cancelTarget) }}.
        The room frees up immediately and the booker is notified.
      </p>
    </Modal>
  </div>
</template>

<style scoped>
.datebar { display: flex; align-items: center; gap: 8px; margin-bottom: 12px; }
.datebar .field { padding: 6px 9px; }
.datelabel { font-size: 13px; font-weight: 600; margin-left: 4px; }
.strong { font-weight: 600; }
.mutedc { color: var(--muted); }
.actions { text-align: right; white-space: nowrap; }
.dl { display: grid; gap: 9px; }
.dl > div { display: flex; align-items: center; justify-content: space-between; gap: 12px; font-size: 13px; }
.dl span:first-child { color: var(--muted); }
.detail-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px; }
.form { display: grid; gap: 12px; }
.form label { display: grid; gap: 5px; font-size: 12px; color: var(--muted); font-weight: 500; }
.two { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.ferr { color: var(--danger); font-size: 12.5px; }
.factions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 4px; }
.confirm-text { margin: 0; font-size: 13px; color: var(--muted); }
@media (max-width: 560px) { .two { grid-template-columns: 1fr; } }
</style>
