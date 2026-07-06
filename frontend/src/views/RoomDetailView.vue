<script setup lang="ts">
import { ref, computed, watch, watchEffect } from 'vue'
import { useRoute } from 'vue-router'
import { usePoll } from '@/composables/usePoll'
import { getRooms, getOccupancy, getBeacons, getEvents, getReservations, adminCreateReservation } from '@/api/client'
import { useToast } from '@/composables/useToast'
import type { Room, OccupancyEntry, Beacon, EventEntry, Reservation } from '@/api/types'
import SegmentedControl from '@/components/ui/SegmentedControl.vue'
import RoomFormModal from '@/components/rooms/RoomFormModal.vue'
import WeekGrid from '@/components/schedule/WeekGrid.vue'
import Modal from '@/components/ui/Modal.vue'

const route = useRoute()
const toast = useToast()

const ws = computed(() => String(route.params.ws ?? ''))

const rooms = ref<Room[]>([])
const occupancy = ref<OccupancyEntry[]>([])
const beacons = ref<Beacon[]>([])
const loaded = ref(false)

const room = computed(() => rooms.value.find(r => r.zoom_workspace_id === ws.value) ?? null)
const beacon = computed(() => beacons.value.find(b => b.workspace_id === ws.value) ?? null)
const presentUsers = computed(() => occupancy.value.find(o => o.workspace_id === ws.value)?.users ?? [])

watchEffect(() => { document.title = `QuickRoom · ${room.value?.name ?? 'Room'}` })

// timeline
const tlDate = ref(new Date())
const tlDetail = ref<Reservation | null>(null)
const createOpen = ref(false)
const createStart = ref('')
const createEnd = ref('')
const createEmail = ref('')
const createError = ref('')
const createBusy = ref(false)

// Monday of tlDate's week, 00:00 local.
const weekStart = computed(() => {
  const d = new Date(tlDate.value)
  d.setHours(0, 0, 0, 0)
  d.setDate(d.getDate() - ((d.getDay() + 6) % 7))
  return d
})
const weekLabel = computed(() => {
  const end = new Date(weekStart.value)
  end.setDate(end.getDate() + 6)
  const f = (d: Date) => d.toLocaleDateString([], { month: 'short', day: 'numeric' })
  return `${f(weekStart.value)} – ${f(end)}`
})
function stepWeek(delta: number) {
  const d = new Date(tlDate.value)
  d.setDate(d.getDate() + delta * 7)
  tlDate.value = d
}
function toLocalDate(d: Date) {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}
function toLocalInput(d: Date) {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${toLocalDate(d)}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function openCreate(slot: { start: Date; end: Date }) {
  createStart.value = toLocalInput(slot.start)
  createEnd.value = toLocalInput(slot.end)
  createEmail.value = ''
  createError.value = ''
  createOpen.value = true
}

async function submitCreate() {
  createBusy.value = true
  createError.value = ''
  try {
    await adminCreateReservation({
      workspace_id: ws.value,
      start_time: new Date(createStart.value).toISOString(),
      end_time: new Date(createEnd.value).toISOString(),
      user_email: createEmail.value || undefined,
    })
    toast.success('Room booked')
    createOpen.value = false
    await loadHistory()
  } catch (e) {
    createError.value = e instanceof Error ? e.message : 'request failed'
  } finally {
    createBusy.value = false
  }
}

// history
const tab = ref('activity')
const events = ref<EventEntry[]>([])
const fetchedAt = ref(0) // ms epoch of the events fetch; ago_sec is relative to it
const bookings = ref<Reservation[]>([])
const historyLoading = ref(false)
const actSearch = ref('')
const actKind = ref('all')
const actSort = ref('newest')
const bookSearch = ref('')
const bookStatus = ref('all')
const bookSort = ref('newest')

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

function eventDate(e: EventEntry) { return new Date(fetchedAt.value - e.ago_sec * 1000) }
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

const filteredEvents = computed(() => {
  const q = actSearch.value.trim().toLowerCase()
  const rows = events.value.filter(e => {
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
  const rows = bookings.value.filter(b => {
    if (bookStatus.value !== 'all' && bookingBadge(b).label !== bookStatus.value) return false
    if (!q) return true
    return `${b.user_email} ${b.user_id}`.toLowerCase().includes(q)
  })
  const dir = bookSort.value === 'newest' ? -1 : 1
  return [...rows].sort((a, b) => dir * (new Date(a.start_time).getTime() - new Date(b.start_time).getTime()))
})
const bookingStatusOptions = computed(() => {
  const seen = new Set<string>()
  for (const b of bookings.value) seen.add(bookingBadge(b).label)
  return [...seen].sort()
})

async function loadHistory() {
  historyLoading.value = true
  try {
    const [ev, all] = await Promise.all([getEvents(ws.value, 200), getReservations()])
    events.value = ev
    fetchedAt.value = Date.now()
    bookings.value = all.filter(b => b.zoom_workspace_id === ws.value)
  } catch (e) {
    toast.error(e instanceof Error ? e.message : 'could not load history')
  } finally {
    historyLoading.value = false
  }
}

async function refresh() {
  const [r, occ, b] = await Promise.all([getRooms(), getOccupancy(), getBeacons()])
  rooms.value = r; occupancy.value = occ; beacons.value = b
  loaded.value = true
}
usePoll(() => refresh().catch(() => {}), 4000)
watch(ws, () => { loadHistory() }, { immediate: true })

// edit modal
const formOpen = ref(false)
</script>

<template>
  <div>
    <router-link class="back" to="/rooms">&#8249; Rooms</router-link>

    <div v-if="!loaded" class="empty">Loading&#8230;</div>
    <div v-else-if="!room" class="empty"><b>Room not found.</b>It may have been removed or the link is stale.</div>

    <template v-else>
      <header class="vh">
        <div>
          <h1>{{ room.name }}</h1>
          <p class="sub room-meta">
            <span class="meta-fact">{{ room.capacity }} seats</span>
            <span v-if="room.is_zoom_room" class="badge b-blue" title="Supports Zoom app meetings">Zoom Room</span>
            <span v-if="room.has_tv" class="badge b-muted">TV</span>
            <span v-if="beacon" class="meta-fact mono">beacon {{ beacon.major }}/{{ beacon.minor }}</span>
          </p>
        </div>
        <div class="vh-actions">
          <button class="btn-secondary" @click="loadHistory()">Reload history</button>
          <button class="btn-primary" @click="formOpen = true">Edit room</button>
        </div>
      </header>

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
        v-model="tab"
        :options="[
          { value: 'timeline', label: 'Timeline' },
          { value: 'activity', label: `Activity (${events.length})` },
          { value: 'bookings', label: `Bookings (${bookings.length})` },
        ]"
      />

      <div v-if="historyLoading" class="empty">Loading&#8230;</div>

      <template v-else-if="tab === 'timeline'">
        <div class="datebar">
          <button class="btn-secondary" aria-label="Previous week" @click="stepWeek(-1)">&#8249;</button>
          <span class="datelabel">{{ weekLabel }}</span>
          <button class="btn-secondary" aria-label="Next week" @click="stepWeek(1)">&#8250;</button>
          <button class="btn-ghost" @click="tlDate = new Date()">This week</button>
        </div>
        <WeekGrid
          :reservations="bookings"
          :week-start="weekStart"
          @select="tlDetail = $event"
          @create="openCreate($event)"
        />
      </template>

      <div v-else class="card pad">
        <template v-if="tab === 'activity'">
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
            <span class="showing">{{ filteredEvents.length }} of {{ events.length }}</span>
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
              {{ events.length ? 'Nothing matches the filters.' : 'No presence activity recorded yet.' }}
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
            <span class="showing">{{ filteredBookings.length }} of {{ bookings.length }}</span>
          </div>
          <div class="hist">
            <div v-for="b in filteredBookings" :key="b.reservation_id" class="hrow">
              <span class="badge" :class="bookingBadge(b).tone">{{ bookingBadge(b).label }}</span>
              <span class="who">{{ b.user_email || b.user_id || 'unknown' }}</span>
              <span class="when mono">{{ fmtWindow(b) }}</span>
            </div>
            <div v-if="!filteredBookings.length" class="none">
              {{ bookings.length ? 'Nothing matches the filters.' : 'No bookings for this room yet.' }}
            </div>
          </div>
        </template>
      </div>

      <RoomFormModal :open="formOpen" :room="room" :beacon="beacon" @close="formOpen = false" @saved="refresh" />

      <Modal :title="room.name" :open="tlDetail !== null" @close="tlDetail = null">
        <div v-if="tlDetail" class="dl">
          <div><span>Booker</span><b>{{ tlDetail.user_email || tlDetail.user_id || 'unknown' }}</b></div>
          <div><span>Window</span><b>{{ fmtWindow(tlDetail) }}</b></div>
          <div><span>Status</span><span class="badge" :class="bookingBadge(tlDetail).tone">{{ bookingBadge(tlDetail).label }}</span></div>
          <div><span>Source</span><b>{{ tlDetail.source || 'zoom' }}</b></div>
        </div>
      </Modal>

      <Modal title="New booking" :open="createOpen" @close="createOpen = false">
        <form class="form" @submit.prevent="submitCreate">
          <div class="two">
            <label><span>Starts</span><input v-model="createStart" class="field" type="datetime-local" required /></label>
            <label><span>Ends</span><input v-model="createEnd" class="field" type="datetime-local" required /></label>
          </div>
          <label>
            <span>Booker email (optional)</span>
            <input v-model.trim="createEmail" class="field" type="email" placeholder="Used for their notifications" />
          </label>
          <div v-if="createError" class="ferr">{{ createError }}</div>
          <div class="factions">
            <button type="button" class="btn-secondary" @click="createOpen = false">Cancel</button>
            <button type="submit" class="btn-primary" :disabled="createBusy || !createStart || !createEnd">Book room</button>
          </div>
        </form>
      </Modal>
    </template>
  </div>
</template>

<style scoped>
.back { display: inline-block; font-size: 13px; font-weight: 500; color: var(--accent); margin-bottom: 10px; }
.room-meta { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.meta-fact { font-size: 12.5px; color: var(--muted); }
.mono { font-variant-numeric: tabular-nums; }

.present-card { border: 1px solid var(--line-soft); border-radius: 12px; padding: 12px 14px; margin-bottom: 14px; background: var(--panel); }
.present-card.live { border-color: rgba(52, 199, 89, .35); background: rgba(52, 199, 89, .05); }
.present-head { display: flex; align-items: center; gap: 7px; font-size: 12.5px; font-weight: 600; color: var(--muted); }
.present-card.live .present-head { color: #1d8a3e; }
.present-head .led { width: 7px; height: 7px; border-radius: 50%; background: var(--faint); flex: none; }
.present-head .led.on { background: var(--signal); box-shadow: 0 0 6px rgba(52, 199, 89, .7); }
.present-chips { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 9px; }
.chip { font-size: 12px; font-weight: 500; padding: 3px 10px; border-radius: 999px; background: #fff;
  border: 1px solid var(--line); }

.pad { padding: 4px 16px 10px; margin-top: 12px; }
.datebar { display: flex; align-items: center; gap: 8px; margin: 12px 0; }
.datebar .field { padding: 6px 9px; width: auto; }
.datelabel { font-size: 13px; font-weight: 600; margin-left: 4px; }
.dl { display: grid; gap: 9px; }
.dl > div { display: flex; align-items: center; justify-content: space-between; gap: 12px; font-size: 13px; }
.dl span:first-child { color: var(--muted); }
.form { display: grid; gap: 12px; }
.form label { display: grid; gap: 5px; font-size: 12px; color: var(--muted); font-weight: 500; }
.two { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.ferr { color: var(--danger); font-size: 12.5px; }
.factions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 4px; }
.explore-bar { display: flex; align-items: center; gap: 8px; margin: 12px 0 6px; }
.explore-bar .field { padding: 6px 10px; font-size: 12.5px; min-width: 0; flex: 1; }
.explore-bar .pick { flex: none; width: auto; max-width: 150px; }
.showing { flex: none; font-size: 11.5px; color: var(--faint); font-variant-numeric: tabular-nums; }

.hist { display: grid; gap: 0; }
.day { position: sticky; top: 0; background: var(--panel); font-size: 11px; font-weight: 700;
  text-transform: uppercase; letter-spacing: .4px; color: var(--faint); padding: 10px 4px 5px;
  border-bottom: 1px solid var(--line-soft); }
.hrow { display: flex; align-items: center; gap: 10px; padding: 9px 4px; font-size: 13px;
  border-bottom: 1px solid var(--line-soft); }
.hrow:last-child { border-bottom: none; }
.hrow .who { font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.hrow .when { margin-left: auto; color: var(--muted); font-size: 12px; white-space: nowrap; }
.none { color: var(--muted); text-align: center; padding: 22px 0; font-size: 13px; }
</style>
