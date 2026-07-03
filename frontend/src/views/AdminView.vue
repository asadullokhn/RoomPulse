<script setup lang="ts">
import { ref, computed } from 'vue'
import AppHeader from '@/components/AppHeader.vue'
import DataTable from '@/components/DataTable.vue'
import Badge from '@/components/Badge.vue'
import KpiRow from '@/components/admin/KpiRow.vue'
import AlertsList from '@/components/admin/AlertsList.vue'
import RoomsGrid from '@/components/admin/RoomsGrid.vue'
import NotificationsList from '@/components/admin/NotificationsList.vue'
import { usePoll } from '@/composables/usePoll'
import { useConnection } from '@/composables/useConnection'
import { getUtilization, getReservations, getRooms, getOccupancy, getCollisions, getOverstays, getNotifications } from '@/api/client'
import type { Utilization, Reservation, Room, OccupancyEntry, Collision, Overstay, Notification } from '@/api/types'

document.title = 'QuickRoom · Admin'

const { connected, markUp, markDown } = useConnection()
const util = ref<Utilization>({ bookings: 0, checked_in: 0, no_show_released: 0, booked: 0, no_show_rate: 0, rooms_total: 0, rooms_occupied: 0, people_present: 0, generated_at: '' })
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

function roomName(ws: string) { return rooms.value.find(r => r.zoom_workspace_id === ws)?.name ?? ws }
function occCount(ws: string) { return occByWs.value[ws]?.count ?? 0 }
function fmtTime(s?: string) { return s ? new Date(s).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '—' }
function statusTone(s: string): 'muted' | 'danger' | 'signal' { return s === 'released' ? 'muted' : s === 'no_show' ? 'danger' : 'signal' }
function checkTone(s: string): 'signal' | 'muted' | 'amber' { return s === 'checked_in' ? 'signal' : s === 'checked_out' ? 'muted' : 'amber' }
function checkLabel(s: string) { return s === 'checked_in' ? 'checked in' : s === 'checked_out' ? 'checked out' : 'awaiting' }

async function refresh() {
  try {
    const [u, res, r, occ, col, over, notes] = await Promise.all([
      getUtilization(), getReservations(), getRooms(), getOccupancy(), getCollisions(), getOverstays(), getNotifications(30),
    ])
    util.value = u; reservations.value = res; rooms.value = r; occupancy.value = occ
    collisions.value = col; overstays.value = over; notifications.value = notes
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
          <DataTable :columns="['Room', 'Booker', 'Window', 'Status', 'Check-in', 'Present']" :rows="reservations"
            empty-title="No reservations" empty-body="No reservations in the window.">
            <tr v-for="r in reservations" :key="r.reservation_id">
              <td class="room-cell">{{ roomName(r.zoom_workspace_id) }}</td>
              <td class="muted">{{ r.user_email || r.user_id || '—' }}</td>
              <td class="mono muted">{{ fmtTime(r.start_time) }}–{{ fmtTime(r.end_time) }}</td>
              <td><Badge :tone="statusTone(r.status)">{{ r.status }}</Badge></td>
              <td><Badge :tone="checkTone(r.check_in_status)">{{ checkLabel(r.check_in_status) }}</Badge></td>
              <td class="mono">{{ occCount(r.zoom_workspace_id) }}</td>
            </tr>
          </DataTable>
        </section>

        <section class="block">
          <div class="eyebrow"><span class="n">03</span> Rooms &amp; occupancy <span class="aside">live headcount</span></div>
          <RoomsGrid :rooms="rooms" :occupancy-by-ws="occByWs" />
        </section>

        <section class="block">
          <div class="eyebrow"><span class="n">04</span> Notification outbox <span class="aside">{{ notifications.length }} recent</span></div>
          <NotificationsList :notifications="notifications" />
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
</style>
