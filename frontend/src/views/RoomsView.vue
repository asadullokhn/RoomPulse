<script setup lang="ts">
import { ref, computed } from 'vue'
import { usePoll } from '@/composables/usePoll'
import { getRooms, getOccupancy, getBeacons, getEvents, getReservations, createRoom, patchRoom, putBeacon, deleteBeacon } from '@/api/client'
import { useToast } from '@/composables/useToast'
import type { Room, OccupancyEntry, Beacon, EventEntry, Reservation } from '@/api/types'
import Toolbar from '@/components/ui/Toolbar.vue'
import Modal from '@/components/ui/Modal.vue'
import SegmentedControl from '@/components/ui/SegmentedControl.vue'

document.title = 'QuickRoom · Rooms'

// The fleet's shared iBeacon proximity UUID — rooms differ by major/minor.
const DEFAULT_BEACON_UUID = '11111111-2222-3333-4444-555555555555'

const toast = useToast()
const rooms = ref<Room[]>([])
const occupancy = ref<OccupancyEntry[]>([])
const beacons = ref<Beacon[]>([])
const loaded = ref(false)
const search = ref('')

// room explorer modal
const historyRoom = ref<Room | null>(null)
const historyTab = ref('activity')
const historyEvents = ref<EventEntry[]>([])
const historyFetchedAt = ref(0) // ms epoch when events were fetched (ago_sec is relative to it)
const historyBookings = ref<Reservation[]>([])
const historyLoading = ref(false)
const actSearch = ref('')
const actKind = ref('all')
const actSort = ref('newest')
const bookSearch = ref('')
const bookStatus = ref('all')
const bookSort = ref('newest')

// add/edit modal
const formOpen = ref(false)
const editingWs = ref('')
const formName = ref('')
const formCapacity = ref(4)
const formTv = ref(false)
const formBeaconUuid = ref('')
const formBeaconMajor = ref<number | null>(null)
const formBeaconMinor = ref<number | null>(null)
const formError = ref('')
const busy = ref(false)

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

// ---------- room explorer ----------

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

function eventDate(e: EventEntry) { return new Date(historyFetchedAt.value - e.ago_sec * 1000) }
function agoLabel(sec: number) {
  if (sec < 60) return `${sec}s ago`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ago`
  return `${Math.floor(sec / 86400)}d ago`
}
function clockLabel(d: Date) { return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) }
function dayLabel(d: Date) {
  const today = new Date(); today.setHours(0, 0, 0, 0)
  const day = new Date(d); day.setHours(0, 0, 0, 0)
  const diff = Math.round((today.getTime() - day.getTime()) / 86400000)
  if (diff === 0) return 'Today'
  if (diff === 1) return 'Yesterday'
  return d.toLocaleDateString([], { weekday: 'long', month: 'short', day: 'numeric' })
}
function fmtWindow(r: Reservation) {
  const f = (s: string) => new Date(s).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  return `${f(r.start_time)} – ${new Date(r.end_time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`
}

const presentUsers = computed(() => {
  if (!historyRoom.value) return []
  return occupancy.value.find(o => o.workspace_id === historyRoom.value!.zoom_workspace_id)?.users ?? []
})

const filteredEvents = computed(() => {
  const q = actSearch.value.trim().toLowerCase()
  const rows = historyEvents.value.filter(e => {
    if (actKind.value !== 'all' && e.kind !== actKind.value) return false
    if (!q) return true
    return `${e.name} ${e.actor}`.toLowerCase().includes(q)
  })
  return actSort.value === 'newest' ? rows : [...rows].reverse()
})

// Consecutive same-day events collapse under one date header.
const groupedEvents = computed(() => {
  const groups: { day: string; items: EventEntry[] }[] = []
  for (const e of filteredEvents.value) {
    const day = dayLabel(eventDate(e))
    const last = groups[groups.length - 1]
    if (last && last.day === day) last.items.push(e)
    else groups.push({ day, items: [e] })
  }
  return groups
})

const filteredBookings = computed(() => {
  const q = bookSearch.value.trim().toLowerCase()
  const rows = historyBookings.value.filter(b => {
    if (bookStatus.value !== 'all' && bookingBadge(b).label !== bookStatus.value) return false
    if (!q) return true
    return `${b.user_email} ${b.user_id}`.toLowerCase().includes(q)
  })
  const dir = bookSort.value === 'newest' ? -1 : 1
  return [...rows].sort((a, b) => dir * (new Date(a.start_time).getTime() - new Date(b.start_time).getTime()))
})
const bookingStatusOptions = computed(() => {
  const seen = new Set<string>()
  for (const b of historyBookings.value) seen.add(bookingBadge(b).label)
  return [...seen].sort()
})

async function openHistory(r: Room) {
  historyRoom.value = r
  historyTab.value = 'activity'
  historyLoading.value = true
  historyEvents.value = []
  historyBookings.value = []
  actSearch.value = ''; actKind.value = 'all'; actSort.value = 'newest'
  bookSearch.value = ''; bookStatus.value = 'all'; bookSort.value = 'newest'
  try {
    const [events, all] = await Promise.all([getEvents(r.zoom_workspace_id, 200), getReservations()])
    historyEvents.value = events
    historyFetchedAt.value = Date.now()
    historyBookings.value = all.filter(b => b.zoom_workspace_id === r.zoom_workspace_id)
  } catch (e) {
    toast.error(e instanceof Error ? e.message : 'could not load history')
  } finally {
    historyLoading.value = false
  }
}

// ---------- add / edit ----------

function openAdd() {
  editingWs.value = ''
  formName.value = ''
  formCapacity.value = 4
  formTv.value = false
  formBeaconUuid.value = DEFAULT_BEACON_UUID
  formBeaconMajor.value = null
  formBeaconMinor.value = null
  formError.value = ''
  formOpen.value = true
}
function openEdit(r: Room) {
  editingWs.value = r.zoom_workspace_id
  formName.value = r.name
  formCapacity.value = r.capacity
  formTv.value = r.has_tv
  const b = beaconByWs.value[r.zoom_workspace_id]
  formBeaconUuid.value = b?.uuid ?? DEFAULT_BEACON_UUID
  formBeaconMajor.value = b?.major ?? null
  formBeaconMinor.value = b?.minor ?? null
  formError.value = ''
  formOpen.value = true
}

async function submitForm() {
  busy.value = true
  formError.value = ''
  try {
    let ws = editingWs.value
    if (ws) {
      await patchRoom(ws, { name: formName.value, capacity: formCapacity.value, has_tv: formTv.value })
    } else {
      await createRoom({ name: formName.value, capacity: formCapacity.value, has_tv: formTv.value })
      // resolve the new room's workspace id by name to attach the beacon
      const fresh = await getRooms()
      ws = fresh.find(r => r.name === formName.value)?.zoom_workspace_id ?? ''
    }

    const hadBeacon = !!beaconByWs.value[ws]
    const wantsBeacon = formBeaconMajor.value !== null && formBeaconMinor.value !== null && formBeaconUuid.value.trim() !== ''
    if (ws && wantsBeacon) {
      await putBeacon(ws, { uuid: formBeaconUuid.value.trim(), major: formBeaconMajor.value!, minor: formBeaconMinor.value! })
    } else if (ws && hadBeacon && !wantsBeacon) {
      await deleteBeacon(ws)
    }

    toast.success(editingWs.value ? 'Room updated' : 'Room added')
    formOpen.value = false
    await refresh()
  } catch (e) {
    formError.value = e instanceof Error ? e.message : 'request failed'
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
            <tr><th>Name</th><th>Capacity</th><th>Amenities</th><th>Present</th><th>Beacon</th><th></th></tr>
          </thead>
          <tbody>
            <tr v-for="r in filtered" :key="r.zoom_workspace_id" class="rowlink" @click="openHistory(r)">
              <td class="strong">{{ r.name }}</td>
              <td class="num">{{ r.capacity }}</td>
              <td class="amenities">
                <span v-if="r.is_zoom_room" class="badge b-blue" title="Supports Zoom app meetings">Zoom Room</span>
                <span v-if="r.has_tv" class="badge b-muted">TV</span>
                <span v-if="isCustom(r.zoom_workspace_id)" class="badge b-amber">Custom</span>
                <span v-if="!r.is_zoom_room && !r.has_tv && !isCustom(r.zoom_workspace_id)" class="mutedc">—</span>
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
                <button class="btn-ghost" @click.stop="openEdit(r)">Edit</button>
              </td>
            </tr>
            <tr v-if="!filtered.length">
              <td colspan="6" class="empty"><b>No rooms match.</b>Try a different search.</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <Modal :title="historyRoom?.name ?? ''" :open="historyRoom !== null" wide @close="historyRoom = null">
      <template v-if="historyRoom">
        <div class="room-meta">
          <span class="meta-fact">{{ historyRoom.capacity }} seats</span>
          <span v-if="historyRoom.is_zoom_room" class="badge b-blue" title="Supports Zoom app meetings">Zoom Room</span>
          <span v-if="historyRoom.has_tv" class="badge b-muted">TV</span>
          <span v-if="beaconByWs[historyRoom.zoom_workspace_id]" class="meta-fact mono">
            beacon {{ beaconByWs[historyRoom.zoom_workspace_id].major }}/{{ beaconByWs[historyRoom.zoom_workspace_id].minor }}
          </span>
          <button class="btn-ghost meta-edit" @click="openEdit(historyRoom)">Edit room</button>
        </div>

        <div class="present-card" :class="{ live: presentUsers.length }">
          <div class="present-head">
            <span class="led" :class="{ on: presentUsers.length }" />
            {{ presentUsers.length ? `In the room now — ${presentUsers.length}` : 'Empty right now' }}
          </div>
          <div v-if="presentUsers.length" class="present-chips">
            <span v-for="u in presentUsers" :key="u" class="chip">{{ u }}</span>
          </div>
        </div>

        <SegmentedControl
          v-model="historyTab"
          :options="[
            { value: 'activity', label: `Activity (${historyEvents.length})` },
            { value: 'bookings', label: `Bookings (${historyBookings.length})` },
          ]"
        />

        <div v-if="historyLoading" class="empty">Loading&#8230;</div>

        <template v-else-if="historyTab === 'activity'">
          <div class="explore-bar">
            <input v-model="actSearch" class="field" placeholder="Search person" />
            <select v-model="actKind" class="field pick">
              <option value="all">All events</option>
              <option value="enter">Entered</option>
              <option value="leave">Left</option>
            </select>
            <select v-model="actSort" class="field pick">
              <option value="newest">Newest first</option>
              <option value="oldest">Oldest first</option>
            </select>
            <span class="showing">{{ filteredEvents.length }} of {{ historyEvents.length }}</span>
          </div>
          <div class="hist">
            <template v-for="g in groupedEvents" :key="g.day + g.items.length">
              <div class="day">{{ g.day }}</div>
              <div v-for="(e, i) in g.items" :key="g.day + i" class="hrow">
                <span class="badge" :class="e.kind === 'enter' ? 'b-signal' : 'b-muted'">{{ e.kind === 'enter' ? 'entered' : 'left' }}</span>
                <span class="who">{{ e.name || e.actor }}</span>
                <span class="when mono">{{ clockLabel(eventDate(e)) }} · {{ agoLabel(e.ago_sec) }}</span>
              </div>
            </template>
            <div v-if="!filteredEvents.length" class="none">
              {{ historyEvents.length ? 'Nothing matches the filters.' : 'No presence activity recorded yet.' }}
            </div>
          </div>
        </template>

        <template v-else>
          <div class="explore-bar">
            <input v-model="bookSearch" class="field" placeholder="Search booker" />
            <select v-model="bookStatus" class="field pick">
              <option value="all">All statuses</option>
              <option v-for="st in bookingStatusOptions" :key="st" :value="st">{{ st }}</option>
            </select>
            <select v-model="bookSort" class="field pick">
              <option value="newest">Newest first</option>
              <option value="oldest">Oldest first</option>
            </select>
            <span class="showing">{{ filteredBookings.length }} of {{ historyBookings.length }}</span>
          </div>
          <div class="hist">
            <div v-for="b in filteredBookings" :key="b.reservation_id" class="hrow">
              <span class="badge" :class="bookingBadge(b).tone">{{ bookingBadge(b).label }}</span>
              <span class="who">{{ b.user_email || b.user_id || 'unknown' }}</span>
              <span class="when mono">{{ fmtWindow(b) }}</span>
            </div>
            <div v-if="!filteredBookings.length" class="none">
              {{ historyBookings.length ? 'Nothing matches the filters.' : 'No bookings for this room yet.' }}
            </div>
          </div>
        </template>
      </template>
    </Modal>

    <Modal :title="editingWs ? 'Edit room' : 'Add room'" :open="formOpen" @close="formOpen = false">
      <form class="form" @submit.prevent="submitForm">
        <label><span>Name</span><input v-model.trim="formName" class="field" required /></label>
        <label><span>Capacity</span><input v-model.number="formCapacity" class="field" type="number" min="0" required /></label>
        <label class="check"><input v-model="formTv" type="checkbox" /><span>Has a TV</span></label>

        <div class="beacon-block">
          <div class="bb-title">Beacon</div>
          <label><span>Proximity UUID</span><input v-model.trim="formBeaconUuid" class="field mono" placeholder="Proximity UUID" /></label>
          <div class="two">
            <label><span>Major</span><input v-model.number="formBeaconMajor" class="field" type="number" min="0" max="65535" placeholder="—" /></label>
            <label><span>Minor</span><input v-model.number="formBeaconMinor" class="field" type="number" min="0" max="65535" placeholder="—" /></label>
          </div>
          <p class="hint">Major + minor identify this room's beacon. Clear both to detach the beacon.</p>
        </div>

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
  </div>
</template>

<style scoped>
.strong { font-weight: 600; }
.rowlink { cursor: pointer; }
.rowlink:hover td { background: rgba(0, 0, 0, .02); }
/* Not flex: a td with display:flex leaves the table-cell layout and its row
   border disappears. Badges are inline-block already. */
.amenities { white-space: nowrap; }
.amenities .badge + .badge { margin-left: 5px; }
.mutedc { color: var(--muted); }
.actions { text-align: right; white-space: nowrap; }
.occ { font-variant-numeric: tabular-nums; color: var(--muted); }
.occ.on { color: #1d8a3e; font-weight: 600; }
.mono { font-variant-numeric: tabular-nums; }

.room-meta { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-bottom: 12px; }
.meta-fact { font-size: 12.5px; color: var(--muted); }
.meta-edit { margin-left: auto; }
.present-card { border: 1px solid var(--line-soft); border-radius: 11px; padding: 11px 13px; margin-bottom: 14px; }
.present-card.live { border-color: rgba(52, 199, 89, .35); background: rgba(52, 199, 89, .05); }
.present-head { display: flex; align-items: center; gap: 7px; font-size: 12.5px; font-weight: 600; color: var(--muted); }
.present-card.live .present-head { color: #1d8a3e; }
.present-head .led { width: 7px; height: 7px; border-radius: 50%; background: var(--faint); flex: none; }
.present-head .led.on { background: var(--signal); box-shadow: 0 0 6px rgba(52, 199, 89, .7); }
.present-chips { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 9px; }
.chip { font-size: 12px; font-weight: 500; padding: 3px 10px; border-radius: 999px; background: #fff;
  border: 1px solid var(--line); }

.explore-bar { display: flex; align-items: center; gap: 8px; margin: 14px 0 8px; }
.explore-bar .field { padding: 6px 10px; font-size: 12.5px; min-width: 0; flex: 1; }
.explore-bar .pick { flex: none; width: auto; max-width: 150px; }
.showing { flex: none; font-size: 11.5px; color: var(--faint); font-variant-numeric: tabular-nums; }

.hist { max-height: 46vh; overflow-y: auto; display: grid; gap: 0; }
.day { position: sticky; top: 0; background: var(--panel); font-size: 11px; font-weight: 700;
  text-transform: uppercase; letter-spacing: .4px; color: var(--faint); padding: 10px 4px 5px;
  border-bottom: 1px solid var(--line-soft); }
.hrow { display: flex; align-items: center; gap: 10px; padding: 8px 4px; font-size: 13px;
  border-bottom: 1px solid var(--line-soft); }
.hrow:last-child { border-bottom: none; }
.hrow .who { font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.hrow .when { margin-left: auto; color: var(--muted); font-size: 12px; white-space: nowrap; }
.none { color: var(--muted); text-align: center; padding: 22px 0; font-size: 13px; }

.form { display: grid; gap: 12px; }
.form label { display: grid; gap: 5px; font-size: 12px; color: var(--muted); font-weight: 500; }
.check { display: flex !important; flex-direction: row; align-items: center; gap: 8px; }
.check input { width: 15px; height: 15px; accent-color: var(--accent); }
.check span { font-size: 13px; color: var(--text); }
.beacon-block { border: 1px solid var(--line-soft); border-radius: 11px; padding: 12px; display: grid; gap: 10px; }
.bb-title { font-size: 12px; font-weight: 700; color: var(--muted); }
.two { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.hint { margin: 0; font-size: 12px; color: var(--faint); }
.ferr { color: var(--danger); font-size: 12.5px; }
.factions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 4px; }
@media (max-width: 560px) { .two { grid-template-columns: 1fr; } }
</style>
