<script setup lang="ts">
import { ref, computed, watch, watchEffect } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { usePoll } from '@/composables/usePoll'
import { getUsers, getUserReservations, getRooms, setUserRating, adminCancelReservation } from '@/api/client'
import { useToast } from '@/composables/useToast'
import type { User, Room, Reservation } from '@/api/types'
import WeekGrid from '@/components/schedule/WeekGrid.vue'

const route = useRoute()
const router = useRouter()
const toast = useToast()

const userId = computed(() => String(route.params.id ?? ''))

const users = ref<User[]>([])
const rooms = ref<Room[]>([])
const bookings = ref<Reservation[]>([])
const loaded = ref(false)
const busy = ref(false)

const user = computed(() => users.value.find(u => u.user_id === userId.value) ?? null)
const rating = computed(() => user.value?.rating ?? null)

watchEffect(() => { document.title = `QuickRoom · ${user.value?.name || 'User'}` })

const roomName = (ws: string) => rooms.value.find(r => r.zoom_workspace_id === ws)?.name || ws
const roomLabel = (r: Reservation) => roomName(r.zoom_workspace_id)

// Rating: >=80 dependable, <50 gets the halved no-show grace.
function ratingTone(v: number) { return v >= 80 ? 'b-signal' : v >= 50 ? 'b-blue' : 'b-danger' }
function ratingWord(v: number) { return v >= 80 ? 'Good' : v >= 50 ? 'Fair' : 'Poor' }

const ratingInput = ref<number | null>(null)
watch(rating, ri => { if (ratingInput.value === null && ri) ratingInput.value = ri.effective }, { immediate: true })

async function pinRating() {
  if (ratingInput.value === null) return
  busy.value = true
  try {
    await setUserRating(userId.value, Math.min(100, Math.max(0, Math.round(ratingInput.value))))
    toast.success('Rating pinned')
    await refresh()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : 'rating update failed')
  } finally {
    busy.value = false
  }
}

async function clearRating() {
  busy.value = true
  try {
    await setUserRating(userId.value, null)
    toast.success('Rating back to auto')
    await refresh()
    ratingInput.value = rating.value?.effective ?? null
  } catch (e) {
    toast.error(e instanceof Error ? e.message : 'rating update failed')
  } finally {
    busy.value = false
  }
}

// week timeline (same pattern as the room page)
const tlDate = ref(new Date())
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

// bookings list
const STATUS: Record<string, { label: string; tone: string }> = {
  booked: { label: 'Booked', tone: 'b-blue' },
  no_show: { label: 'Released', tone: 'b-amber' },
  released: { label: 'Released', tone: 'b-amber' },
  cancelled: { label: 'Cancelled', tone: 'b-danger' },
}
function bookingBadge(r: Reservation) {
  if (r.status === 'booked' && r.check_in_status === 'checked_in') return { label: 'Checked-In', tone: 'b-signal' }
  if (r.status === 'booked' && r.check_in_status === 'checked_out') return { label: 'Checked out', tone: 'b-muted' }
  return STATUS[r.status] ?? STATUS.booked
}
function fmtWindow(r: Reservation) {
  const f = (s: string) => new Date(s).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  return `${f(r.start_time)} – ${new Date(r.end_time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`
}
const sortedBookings = computed(() =>
  [...bookings.value].sort((a, b) => Date.parse(b.start_time) - Date.parse(a.start_time)))

async function cancelBooking(reservationId: string) {
  busy.value = true
  try {
    await adminCancelReservation(reservationId)
    toast.success('Booking cancelled')
    await refresh()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : 'cancel failed')
  } finally {
    busy.value = false
  }
}

function fmtJoined(s?: string) {
  return s ? new Date(s).toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' }) : '—'
}

async function refresh() {
  const [u, r] = await Promise.all([getUsers(), getRooms()])
  users.value = u
  rooms.value = r
  if (userId.value) bookings.value = await getUserReservations(userId.value)
  loaded.value = true
}
usePoll(() => refresh().catch(() => {}), 4000)
watch(userId, () => { loaded.value = false; refresh().catch(() => {}) })
</script>

<template>
  <div>
    <router-link class="back" to="/users">&#8249; Users</router-link>

    <div v-if="!loaded" class="empty">Loading&#8230;</div>
    <div v-else-if="!user" class="empty"><b>User not found.</b>The account may have been deleted.</div>

    <template v-else>
      <header class="vh">
        <div>
          <h1>{{ user.name || user.email || user.user_id }}</h1>
          <p class="sub user-meta">
            <span v-if="user.email" class="meta-fact">{{ user.email }}</span>
            <span class="meta-fact">joined {{ fmtJoined(user.created_at) }}</span>
            <span class="meta-fact mono">{{ user.user_id }}</span>
          </p>
        </div>
      </header>

      <div v-if="rating" class="card pad rating-card">
        <div class="rating-head">
          <span class="rating-num badge" :class="ratingTone(rating.effective)">{{ rating.effective }}</span>
          <div>
            <div class="rating-word">
              {{ ratingWord(rating.effective) }}
              <span v-if="rating.override !== undefined" class="badge b-muted">pinned by admin</span>
            </div>
            <div class="rating-sub">
              Showed up {{ rating.good }} &middot; no-shows {{ rating.bad }} &rarr; auto {{ rating.auto }}.
              {{ rating.effective < 50 ? 'No-show grace is halved — their empty bookings release twice as fast.' : 'Standard no-show grace.' }}
            </div>
          </div>
        </div>
        <div class="rating-edit">
          <input v-model.number="ratingInput" class="field num" type="number" min="0" max="100" />
          <button class="btn-secondary" :disabled="busy || ratingInput === null" @click="pinRating">Pin rating</button>
          <button v-if="rating.override !== undefined" class="btn-ghost" :disabled="busy" @click="clearRating">Use auto</button>
        </div>
      </div>

      <div class="datebar">
        <button class="btn-secondary" aria-label="Previous week" @click="stepWeek(-1)">&#8249;</button>
        <span class="datelabel">{{ weekLabel }}</span>
        <button class="btn-secondary" aria-label="Next week" @click="stepWeek(1)">&#8250;</button>
        <button class="btn-ghost" @click="tlDate = new Date()">This week</button>
      </div>
      <WeekGrid
        :reservations="bookings"
        :week-start="weekStart"
        :label="roomLabel"
        readonly
        @select="router.push(`/rooms/${$event.zoom_workspace_id}`)"
      />

      <div class="card scroll bookings-card">
        <table>
          <thead>
            <tr><th>Room</th><th>Window</th><th>Status</th><th>Source</th><th></th></tr>
          </thead>
          <tbody>
            <tr v-for="r in sortedBookings" :key="r.reservation_id">
              <td class="strong">
                <router-link class="room-link" :to="`/rooms/${r.zoom_workspace_id}`">{{ roomName(r.zoom_workspace_id) }}</router-link>
              </td>
              <td class="mutedc">{{ fmtWindow(r) }}</td>
              <td><span class="badge" :class="bookingBadge(r).tone">{{ bookingBadge(r).label }}</span></td>
              <td class="mutedc">{{ r.source || 'zoom' }}</td>
              <td class="actions">
                <button
                  v-if="r.source === 'app' && r.status === 'booked'"
                  class="btn-danger-ghost"
                  :disabled="busy"
                  @click="cancelBooking(r.reservation_id)"
                >Cancel</button>
              </td>
            </tr>
            <tr v-if="!sortedBookings.length">
              <td colspan="5" class="empty"><b>No bookings yet.</b>This account hasn't reserved a room.</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </div>
</template>

<style scoped>
.back { display: inline-block; margin-bottom: 10px; color: var(--muted); text-decoration: none; font-size: 13px; }
.back:hover { color: var(--text); }
.user-meta { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.meta-fact { color: var(--muted); }
.rating-card { display: flex; align-items: center; justify-content: space-between; gap: 16px; flex-wrap: wrap; margin-bottom: 14px; }
.rating-head { display: flex; align-items: center; gap: 14px; }
.rating-num { font-size: 20px; font-weight: 700; padding: 10px 14px; }
.rating-word { font-weight: 600; display: flex; align-items: center; gap: 8px; }
.rating-sub { color: var(--muted); font-size: 13px; margin-top: 2px; }
.rating-edit { display: flex; align-items: center; gap: 8px; }
.num { width: 80px; }
.datebar { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; }
.datelabel { font-weight: 600; min-width: 130px; text-align: center; }
.bookings-card { margin-top: 14px; }
.strong { font-weight: 600; }
.mutedc { color: var(--muted); }
.actions { text-align: right; white-space: nowrap; }
.room-link { color: inherit; text-decoration: none; }
.room-link:hover { text-decoration: underline; }
</style>
