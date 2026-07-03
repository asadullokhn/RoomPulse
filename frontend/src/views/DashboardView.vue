<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import AppHeader from '@/components/AppHeader.vue'
import VitalCard from '@/components/VitalCard.vue'
import DataTable from '@/components/DataTable.vue'
import Badge from '@/components/Badge.vue'
import OccupancyGrid from '@/components/dashboard/OccupancyGrid.vue'
import { usePoll } from '@/composables/usePoll'
import { useConnection } from '@/composables/useConnection'
import { getRooms, getReservations, getOccupancy, getDevices, getBeacons, getInfo, postSync } from '@/api/client'
import type { Room, Reservation, OccupancyEntry, Device, Beacon } from '@/api/types'

document.title = 'QuickRoom'

const { connected, markUp, markDown } = useConnection()
const rooms = ref<Room[]>([])
const reservations = ref<Reservation[]>([])
const occupancy = ref<OccupancyEntry[]>([])
const devices = ref<Device[]>([])
const beacons = ref<Beacon[]>([])
const zoomMode = ref('')
const syncing = ref(false)

const STALE_SEC = 120

const occByWs = computed(() => {
  const m: Record<string, OccupancyEntry> = {}
  for (const o of occupancy.value) m[o.workspace_id] = o
  return m
})
const present = computed(() => rooms.value.reduce((n, r) => n + (occByWs.value[r.zoom_workspace_id]?.count ?? 0), 0))
const occupied = computed(() => rooms.value.filter(r => (occByWs.value[r.zoom_workspace_id]?.count ?? 0) > 0).length)
const occPct = computed(() => rooms.value.length ? Math.round(occupied.value / rooms.value.length * 100) : 0)
const activeDev = computed(() => devices.value.filter(d => d.last_seen_sec <= STALE_SEC).length)

function roomName(ws: string) {
  return rooms.value.find(r => r.zoom_workspace_id === ws)?.name ?? ws
}
function fmtTime(s?: string) {
  if (!s) return '—'
  return new Date(s).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}
function fmtAgo(sec: number | null | undefined) {
  if (sec == null) return '—'
  if (sec < 60) return `${sec}s ago`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  return `${Math.floor(sec / 3600)}h ago`
}
function checkLabel(s: string) {
  return s === 'checked_in' ? 'checked in' : s === 'checked_out' ? 'checked out' : 'awaiting'
}
function checkTone(s: string): 'signal' | 'muted' | 'amber' {
  return s === 'checked_in' ? 'signal' : s === 'checked_out' ? 'muted' : 'amber'
}

async function refresh() {
  try {
    const [r, res, occ, dev, bea] = await Promise.all([
      getRooms(), getReservations(), getOccupancy(), getDevices(), getBeacons(),
    ])
    rooms.value = r; reservations.value = res; occupancy.value = occ; devices.value = dev; beacons.value = bea
    markUp()
  } catch {
    markDown()
  }
}
usePoll(refresh, 3000)

onMounted(async () => {
  try { zoomMode.value = (await getInfo()).zoom_mode } catch { /* leave blank */ }
})

async function sync() {
  syncing.value = true
  try { await postSync() } catch { /* best-effort */ }
  syncing.value = false
  await refresh()
}
</script>

<template>
  <div class="page">
    <AppHeader active="dashboard" :connected="connected" />
    <main>
      <section class="vitals" aria-label="Live vitals">
        <VitalCard label="People present" :value="present" :sub="present === 1 ? '1 person in a room' : 'across all rooms'" tone="hero" />
        <VitalCard label="Rooms occupied" :value="`${occupied}`" tone="default">
          <template #sub><div class="bar"><i :style="{ width: occPct + '%' }" /></div></template>
        </VitalCard>
        <VitalCard label="Active devices" :value="activeDev" :sub="`${devices.length} known · ${devices.length - activeDev} idle`" />
        <VitalCard label="Rooms" :value="rooms.length" :sub="`${beacons.length} beacons mapped`" />
      </section>

      <section class="block">
        <div class="eyebrow">Live occupancy <span class="n">{{ present ? `${present} present` : 'all rooms quiet' }}</span></div>
        <OccupancyGrid :rooms="rooms" :occupancy-by-ws="occByWs" :reservations="reservations" />
      </section>

      <section class="block">
        <div class="eyebrow">Reservations <span class="n">today</span></div>
        <DataTable :columns="['Room', 'Booked by', 'Start', 'End', 'Check-in']" :rows="reservations"
          empty-title="No reservations" empty-body="Nothing booked in the current window.">
          <tr v-for="v in reservations" :key="v.reservation_id">
            <td>{{ roomName(v.zoom_workspace_id) }}</td>
            <td>{{ v.user_email || v.user_id || '—' }}</td>
            <td class="mono">{{ fmtTime(v.start_time) }}</td>
            <td class="mono">{{ fmtTime(v.end_time) }}</td>
            <td><Badge :tone="checkTone(v.check_in_status)">{{ checkLabel(v.check_in_status) }}</Badge></td>
          </tr>
        </DataTable>
      </section>

      <details class="advanced">
        <summary>Advanced · diagnostics</summary>
        <div class="adv-body">
          <div class="adv-row">
            <span class="chip">mode {{ zoomMode }}</span>
            <button class="btn-ghost" :disabled="syncing" @click="sync">{{ syncing ? 'Syncing…' : 'Sync from Zoom' }}</button>
          </div>
          <section class="block">
            <div class="eyebrow">Devices <span class="n">phones reporting presence</span></div>
            <DataTable :columns="['Device', 'Name', 'Room', 'Last seen']" :rows="devices"
              empty-title="No phones yet" empty-body="Open QuickRoom on a phone and turn on auto check-in — it'll appear here within seconds.">
              <tr v-for="d in devices" :key="d.device_id" :class="{ stale: d.last_seen_sec > STALE_SEC }">
                <td class="mono id">{{ d.device_id }}</td>
                <td>{{ d.display_name || '—' }}</td>
                <td><Badge v-if="d.workspace_id" tone="signal">{{ roomName(d.workspace_id) }}</Badge><Badge v-else tone="muted">no room</Badge></td>
                <td class="mono">{{ fmtAgo(d.last_seen_sec) }}</td>
              </tr>
            </DataTable>
          </section>
          <section class="block">
            <div class="eyebrow">Beacons <span class="n">iBeacon identity per room</span></div>
            <DataTable :columns="['Room', 'Workspace', 'UUID', 'Major', 'Minor']" :rows="beacons"
              empty-title="No beacons registered" empty-body="Add a BEACONS_FILE or use the built-in defaults.">
              <tr v-for="b in beacons" :key="b.workspace_id">
                <td>{{ b.name || roomName(b.workspace_id) }}</td>
                <td class="mono id">{{ b.workspace_id }}</td>
                <td class="mono">{{ b.uuid }}</td>
                <td class="num">{{ b.major }}</td>
                <td class="num">{{ b.minor }}</td>
              </tr>
            </DataTable>
          </section>
        </div>
      </details>
      <footer>QuickRoom</footer>
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
.vitals { display: grid; grid-template-columns: repeat(4, 1fr); gap: 14px; margin-bottom: 34px; }
.vital .bar { height: 4px; border-radius: 2px; background: rgba(124,140,170,.18); margin-top: 10px; overflow: hidden; }
.vital .bar i { display: block; height: 100%; background: var(--signal); border-radius: 2px; transition: width .4s ease; }
button { font-family: var(--f-body); font-size: 13px; font-weight: 500; cursor: pointer;
  border-radius: 9px; padding: 8px 14px; border: 1px solid transparent; transition: transform .06s, background .15s, border-color .15s; }
button:active { transform: translateY(1px); }
.btn-ghost { background: transparent; color: var(--text); border-color: var(--line); }
.btn-ghost:hover { border-color: var(--accent); color: #fff; }
.advanced { border-top: 1px solid var(--line); margin-top: 8px; }
.advanced > summary { cursor: pointer; list-style: none; font-family: var(--f-mono); font-size: 11px;
  text-transform: uppercase; letter-spacing: 1.6px; color: var(--muted); padding: 16px 2px; display: flex; align-items: center; gap: 9px; }
.advanced > summary::-webkit-details-marker { display: none; }
.advanced > summary::before { content: "›"; display: inline-block; transition: transform .2s; font-size: 14px; }
.advanced[open] > summary::before { transform: rotate(90deg); }
.advanced > summary:hover { color: var(--text); }
.adv-body { padding-top: 6px; }
.adv-row { display: flex; gap: 10px; align-items: center; margin-bottom: 18px; flex-wrap: wrap; }
footer { text-align: center; color: var(--faint); font-size: 11.5px; font-family: var(--f-mono); padding: 8px 0 0; }
@media (max-width: 860px) { .vitals { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 560px) { main { padding: 20px 14px 48px; } }
</style>
