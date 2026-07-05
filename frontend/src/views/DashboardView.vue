<script setup lang="ts">
import { ref, computed } from 'vue'
import { RouterLink } from 'vue-router'
import { usePoll } from '@/composables/usePoll'
import { getUtilization, getReservations, getRooms, getOccupancy, getCollisions, getOverstays, getNotifications } from '@/api/client'
import type { Utilization, Reservation, Room, OccupancyEntry, Collision, Overstay, Notification } from '@/api/types'

document.title = 'QuickRoom · Dashboard'

const util = ref<Utilization | null>(null)
const reservations = ref<Reservation[]>([])
const rooms = ref<Room[]>([])
const occupancy = ref<OccupancyEntry[]>([])
const collisions = ref<Collision[]>([])
const overstays = ref<Overstay[]>([])
const notifications = ref<Notification[]>([])
const loaded = ref(false)

const occByWs = computed(() => {
  const m: Record<string, OccupancyEntry> = {}
  for (const o of occupancy.value) m[o.workspace_id] = o
  return m
})

const stats = computed(() => util.value ? [
  { label: 'Bookings today', value: String(util.value.bookings) },
  { label: 'Checked in', value: String(util.value.checked_in) },
  { label: 'Auto-released', value: String(util.value.no_show_released) },
  { label: 'No-show rate', value: `${Math.round(util.value.no_show_rate * 100)}%` },
  { label: 'Rooms in use', value: `${util.value.rooms_occupied}/${util.value.rooms_total}` },
  { label: 'People present', value: String(util.value.people_present) },
] : [])

// Fraction of the 07:00-19:00 window covered by booked time today (overlaps merged).
function utilizationToday(ws: string): number {
  const day = new Date(); day.setHours(7, 0, 0, 0)
  const start = day.getTime(), end = start + 12 * 3600_000
  const spans = reservations.value
    .filter(r => r.zoom_workspace_id === ws && r.status === 'booked')
    .map(r => [Math.max(start, Date.parse(r.start_time)), Math.min(end, Date.parse(r.end_time))] as [number, number])
    .filter(([a, b]) => b > a)
    .sort((a, b) => a[0] - b[0])
  let covered = 0, cursor = start
  for (const [a, b] of spans) {
    if (b <= cursor) continue
    covered += b - Math.max(a, cursor)
    cursor = Math.max(cursor, b)
  }
  return covered / (end - start)
}

function occCount(ws: string) { return occByWs.value[ws]?.count ?? 0 }
function fmtClock(s: string) { return s ? new Date(s).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '' }
function noteLabel(t: string) { return (t || '').replace(/_/g, ' ') }

async function refresh() {
  const [u, res, r, occ, col, over, notes] = await Promise.all([
    getUtilization(), getReservations(), getRooms(), getOccupancy(), getCollisions(), getOverstays(), getNotifications(5),
  ])
  util.value = u; reservations.value = res; rooms.value = r; occupancy.value = occ
  collisions.value = col; overstays.value = over; notifications.value = notes
  loaded.value = true
}
usePoll(() => refresh().catch(() => {}), 4000)
</script>

<template>
  <div>
    <header class="vh">
      <div>
        <h1>Dashboard</h1>
        <p class="sub">Live occupancy and booking health across the Academy.</p>
      </div>
    </header>

    <div v-if="!loaded" class="empty">Loading&#8230;</div>
    <template v-else>
      <div class="stats">
        <div v-for="s in stats" :key="s.label" class="card stat">
          <div class="label">{{ s.label }}</div>
          <div class="value">{{ s.value }}</div>
        </div>
      </div>

      <div class="section-label">Needs attention</div>
      <div class="card">
        <div v-if="!collisions.length && !overstays.length" class="empty">All clear. No conflicts or overstays right now.</div>
        <div v-else class="alerts">
          <div v-for="c in collisions" :key="'c' + c.reservation_id" class="alert red">
            <b>{{ c.room_name }}</b> is occupied by someone other than the booker ({{ c.booker || 'unknown' }}).
          </div>
          <div v-for="o in overstays" :key="'o' + o.reservation_id" class="alert orange">
            <b>{{ o.room_name }}</b> is still occupied {{ Math.round(o.over_by_sec / 60) }} min past the booking's end.
          </div>
        </div>
      </div>

      <div class="section-label">Rooms today</div>
      <div class="tiles">
        <div v-for="rm in rooms" :key="rm.zoom_workspace_id" class="card tile">
          <div class="name">
            {{ rm.name }}
            <span class="dot" :class="{ on: occCount(rm.zoom_workspace_id) > 0 }" />
          </div>
          <div class="occ"><b>{{ occCount(rm.zoom_workspace_id) }}</b> / {{ rm.capacity }} present</div>
          <div class="bar">
            <div
              class="fill"
              :class="{ packed: utilizationToday(rm.zoom_workspace_id) > .85 }"
              :style="{ width: `${Math.round(utilizationToday(rm.zoom_workspace_id) * 100)}%` }"
            />
          </div>
          <div class="pct">{{ Math.round(utilizationToday(rm.zoom_workspace_id) * 100) }}% booked today</div>
        </div>
      </div>

      <div class="section-label">Recent activity</div>
      <div class="card">
        <div v-if="!notifications.length" class="empty">No notifications yet.</div>
        <div v-else class="recent">
          <div v-for="n in notifications" :key="n.id" class="row">
            <span class="badge b-muted">{{ noteLabel(n.type) }}</span>
            <span class="txt">{{ n.title }} <span class="body">{{ n.body }}</span></span>
            <span class="time">{{ fmtClock(n.created_at) }}</span>
          </div>
          <RouterLink class="all" to="/notifications">View all notifications</RouterLink>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 10px; }
.stat { padding: 13px 15px; }
.stat .label { font-size: 11px; color: var(--muted); font-weight: 500; }
.stat .value { font-family: var(--f-display); font-size: 26px; font-weight: 700; letter-spacing: -0.02em;
  font-variant-numeric: tabular-nums; margin-top: 2px; }
.alerts { display: grid; }
.alert { padding: 11px 16px; font-size: 13px; border-left: 3px solid; border-bottom: 1px solid var(--line-soft); }
.alert:last-child { border-bottom: 0; }
.alert.red { border-left-color: var(--danger); }
.alert.orange { border-left-color: var(--amber); }
.tiles { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 10px; }
.tile { padding: 13px 15px; }
.tile .name { font-size: 13px; font-weight: 600; display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.dot { width: 7px; height: 7px; border-radius: 50%; background: rgba(0, 0, 0, .12); flex: none; }
.dot.on { background: var(--signal); box-shadow: 0 0 6px rgba(52, 199, 89, .7); }
.occ { font-size: 12px; color: var(--muted); margin-top: 4px; }
.occ b { color: var(--text); font-variant-numeric: tabular-nums; }
.bar { height: 4px; background: #ebebf0; border-radius: 4px; margin-top: 10px; overflow: hidden; }
.fill { height: 100%; background: var(--accent); border-radius: 4px; transition: width .3s ease; }
.fill.packed { background: var(--amber); }
.pct { font-size: 11px; color: var(--faint); margin-top: 5px; font-variant-numeric: tabular-nums; }
.recent { display: grid; }
.recent .row { display: flex; align-items: center; gap: 10px; padding: 10px 16px; border-bottom: 1px solid var(--line-soft); }
.recent .txt { font-size: 13px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.recent .body { color: var(--muted); }
.recent .time { margin-left: auto; font-size: 11.5px; color: var(--faint); font-variant-numeric: tabular-nums; flex: none; }
.all { display: block; padding: 10px 16px; font-size: 13px; font-weight: 500; }
</style>
