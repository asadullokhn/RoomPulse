<script setup lang="ts">
import { ref, computed } from 'vue'
import AppHeader from '@/components/AppHeader.vue'
import DataTable from '@/components/DataTable.vue'
import Badge from '@/components/Badge.vue'
import KpiRow from '@/components/admin/KpiRow.vue'
import AlertsList from '@/components/admin/AlertsList.vue'
import RoomsGrid from '@/components/admin/RoomsGrid.vue'
import NotificationsList from '@/components/admin/NotificationsList.vue'
import BeaconsPanel from '@/components/admin/BeaconsPanel.vue'
import UsersPanel from '@/components/admin/UsersPanel.vue'
import { usePoll } from '@/composables/usePoll'
import { useConnection } from '@/composables/useConnection'
import { getUtilization, getReservations, getRooms, getOccupancy, getCollisions, getOverstays, getNotifications, getBeacons, getUsers, adminCreateReservation, adminPatchReservation, adminCancelReservation } from '@/api/client'
import type { Utilization, Reservation, Room, OccupancyEntry, Collision, Overstay, Notification, Beacon, User } from '@/api/types'

document.title = 'QuickRoom · Admin'

const { connected, markUp, markDown } = useConnection()
const util = ref<Utilization>({ bookings: 0, checked_in: 0, no_show_released: 0, booked: 0, no_show_rate: 0, rooms_total: 0, rooms_occupied: 0, people_present: 0, generated_at: '' })
const reservations = ref<Reservation[]>([])
const rooms = ref<Room[]>([])
const occupancy = ref<OccupancyEntry[]>([])
const collisions = ref<Collision[]>([])
const overstays = ref<Overstay[]>([])
const notifications = ref<Notification[]>([])
const beacons = ref<Beacon[]>([])
const users = ref<User[]>([])
const loaded = ref(false)

const occByWs = computed(() => {
  const m: Record<string, OccupancyEntry> = {}
  for (const o of occupancy.value) m[o.workspace_id] = o
  return m
})

function roomName(ws: string) { return rooms.value.find(r => r.zoom_workspace_id === ws)?.name ?? ws }
function occCount(ws: string) { return occByWs.value[ws]?.count ?? 0 }
function fmtTime(s?: string) { return s ? new Date(s).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '—' }
function statusTone(s: string): 'muted' | 'danger' | 'signal' { return s === 'released' ? 'muted' : s === 'no_show' ? 'danger' : 'signal' }
function checkTone(s: string): 'signal' | 'muted' | 'amber' { return s === 'checked_in' ? 'signal' : s === 'checked_out' ? 'muted' : 'amber' }
function checkLabel(s: string) { return s === 'checked_in' ? 'checked in' : s === 'checked_out' ? 'checked out' : 'awaiting' }

const busy = ref(false)
const bookWs = ref('')
const bookStart = ref('')
const bookEnd = ref('')
const bookEmail = ref('')
const bookError = ref('')
const editId = ref('')
const editStart = ref('')
const editEnd = ref('')

function toLocalInput(s: string) {
  const d = new Date(s)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

async function bookRoom() {
  busy.value = true
  bookError.value = ''
  try {
    await adminCreateReservation({
      workspace_id: bookWs.value,
      start_time: new Date(bookStart.value).toISOString(),
      end_time: new Date(bookEnd.value).toISOString(),
      user_email: bookEmail.value || undefined,
    })
    bookWs.value = ''; bookStart.value = ''; bookEnd.value = ''; bookEmail.value = ''
    await refresh()
  } catch (e) {
    bookError.value = e instanceof Error ? e.message : 'booking failed'
  } finally {
    busy.value = false
  }
}

function startEdit(r: Reservation) {
  editId.value = r.reservation_id
  editStart.value = toLocalInput(r.start_time)
  editEnd.value = toLocalInput(r.end_time)
}

async function saveEdit(id: string) {
  busy.value = true
  try {
    await adminPatchReservation(id, {
      start_time: new Date(editStart.value).toISOString(),
      end_time: new Date(editEnd.value).toISOString(),
    })
    editId.value = ''
    await refresh()
  } catch (e) {
    bookError.value = e instanceof Error ? e.message : 'edit failed'
  } finally {
    busy.value = false
  }
}

async function cancelReservation(id: string) {
  busy.value = true
  try {
    await adminCancelReservation(id)
    await refresh()
  } catch (e) {
    bookError.value = e instanceof Error ? e.message : 'cancel failed'
  } finally {
    busy.value = false
  }
}

async function refresh() {
  try {
    const [u, res, r, occ, col, over, notes, beac, usrs] = await Promise.all([
      getUtilization(), getReservations(), getRooms(), getOccupancy(), getCollisions(), getOverstays(), getNotifications(30), getBeacons(), getUsers(),
    ])
    util.value = u; reservations.value = res; rooms.value = r; occupancy.value = occ
    collisions.value = col; overstays.value = over; notifications.value = notes; beacons.value = beac; users.value = usrs
    markUp(); loaded.value = true
  } catch {
    markDown()
  }
}
usePoll(refresh, 4000)
</script>

<template>
  <div class="page">
    <AppHeader :connected="connected" />
    <main>
      <div v-if="!loaded" class="skeleton">Loading admin data…</div>
      <template v-else>
        <KpiRow :util="util" />

        <section class="block">
          <div class="eyebrow"><span class="n">01</span> Needs attention
            <span class="aside">{{ collisions.length + overstays.length }} open</span>
          </div>
          <AlertsList :collisions="collisions" :overstays="overstays" />
        </section>

        <section class="block">
          <div class="eyebrow"><span class="n">02</span> Reservations</div>
          <form class="book-form" @submit.prevent="bookRoom">
            <select v-model="bookWs" required>
              <option value="" disabled>Room…</option>
              <option v-for="rm in rooms" :key="rm.zoom_workspace_id" :value="rm.zoom_workspace_id">{{ rm.name }}</option>
            </select>
            <input v-model="bookStart" type="datetime-local" required />
            <input v-model="bookEnd" type="datetime-local" required />
            <input v-model.trim="bookEmail" type="email" placeholder="booker email (optional)" />
            <button type="submit" :disabled="busy || !bookWs || !bookStart || !bookEnd">Book</button>
            <span v-if="bookError" class="form-err">{{ bookError }}</span>
          </form>
          <DataTable :columns="['Room', 'Booker', 'Window', 'Status', 'Check-in', 'Present', '']" :rows="reservations"
            empty-title="No reservations" empty-body="No reservations in the window.">
            <tr v-for="r in reservations" :key="r.reservation_id">
              <td class="room-cell">{{ roomName(r.zoom_workspace_id) }}</td>
              <td class="muted">{{ r.user_email || r.user_id || '—' }}</td>
              <td class="mono muted">
                <template v-if="editId === r.reservation_id">
                  <input v-model="editStart" type="datetime-local" class="edit-dt" />
                  <input v-model="editEnd" type="datetime-local" class="edit-dt" />
                </template>
                <template v-else>{{ fmtTime(r.start_time) }}–{{ fmtTime(r.end_time) }}</template>
              </td>
              <td><Badge :tone="statusTone(r.status)">{{ r.status }}</Badge></td>
              <td><Badge :tone="checkTone(r.check_in_status)">{{ checkLabel(r.check_in_status) }}</Badge></td>
              <td class="mono">{{ occCount(r.zoom_workspace_id) }}</td>
              <td class="row-actions">
                <template v-if="r.source === 'app' && r.status === 'booked'">
                  <template v-if="editId === r.reservation_id">
                    <button class="btn-ghost" :disabled="busy" @click.prevent="saveEdit(r.reservation_id)">Save</button>
                    <button class="btn-ghost" @click.prevent="editId = ''">Cancel</button>
                  </template>
                  <template v-else>
                    <button class="btn-ghost" @click.prevent="startEdit(r)">Edit</button>
                    <button class="btn-ghost" :disabled="busy" @click.prevent="cancelReservation(r.reservation_id)">Cancel</button>
                  </template>
                </template>
              </td>
            </tr>
          </DataTable>
        </section>

        <section class="block">
          <div class="eyebrow"><span class="n">03</span> Rooms &amp; occupancy <span class="aside">live headcount</span></div>
          <RoomsGrid :rooms="rooms" :occupancy-by-ws="occByWs" @changed="refresh" />
        </section>

        <section class="block">
          <div class="eyebrow"><span class="n">04</span> Notification outbox <span class="aside">{{ notifications.length }} recent</span></div>
          <NotificationsList :notifications="notifications" @changed="refresh" />
        </section>

        <section class="block">
          <div class="eyebrow"><span class="n">05</span> Beacons <span class="aside">room ↔ iBeacon assignment</span></div>
          <BeaconsPanel :beacons="beacons" :rooms="rooms" @changed="refresh" />
        </section>

        <section class="block">
          <div class="eyebrow"><span class="n">06</span> Users <span class="aside">{{ users.length }} accounts</span></div>
          <UsersPanel :users="users" @changed="refresh" />
        </section>
      </template>
    </main>
  </div>
</template>

<style scoped>
.page {
  background:
    radial-gradient(900px 500px at 82% -8%, rgba(47,230,176,.10), transparent 60%),
    radial-gradient(800px 520px at 10% 0%, rgba(47,230,176,.06), transparent 55%),
    linear-gradient(180deg, var(--ink-2), var(--ink) 38%);
  background-attachment: fixed; min-height: 100vh;
}
main { padding: 26px 24px 60px; max-width: 1180px; margin: 0 auto; }
.eyebrow .aside { margin-left: auto; text-transform: none; letter-spacing: normal; color: var(--muted); font-size: 11px; }
.room-cell { font-weight: 600; }
.muted { color: var(--muted); }
.skeleton { color: var(--faint); text-align: center; padding: 40px; }
.book-form { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; margin-bottom: 12px; }
.book-form select, .book-form input { background: rgba(150,170,220,.05); border: 1px solid var(--line);
  border-radius: 9px; color: var(--ink); padding: 7px 10px; font-size: 12.5px; font-family: var(--f-body); }
.book-form button { background: #2FE6B0; color: #06281e; border: none; border-radius: 9px;
  padding: 8px 16px; font-weight: 700; font-size: 12.5px; cursor: pointer; font-family: var(--f-body); }
.book-form button:disabled { opacity: .55; cursor: default; }
.form-err { color: var(--amber); font-size: 12px; }
.row-actions { white-space: nowrap; text-align: right; }
.btn-ghost { background: none; border: 1px solid var(--line); border-radius: 8px; color: var(--muted);
  padding: 4px 10px; font-size: 11.5px; cursor: pointer; font-family: var(--f-body); }
.btn-ghost:hover { color: var(--ink); border-color: var(--signal-line); }
.btn-ghost + .btn-ghost { margin-left: 6px; }
.edit-dt { background: rgba(150,170,220,.05); border: 1px solid var(--line); border-radius: 7px;
  color: var(--ink); padding: 4px 7px; font-size: 11.5px; font-family: var(--f-mono); }
</style>
