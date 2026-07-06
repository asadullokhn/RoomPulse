<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { usePoll } from '@/composables/usePoll'
import { getRooms, getOccupancy, getBeacons } from '@/api/client'
import type { Room, OccupancyEntry, Beacon } from '@/api/types'
import Toolbar from '@/components/ui/Toolbar.vue'
import RoomFormModal from '@/components/rooms/RoomFormModal.vue'

document.title = 'QuickRoom · Rooms'

const router = useRouter()
const rooms = ref<Room[]>([])
const occupancy = ref<OccupancyEntry[]>([])
const beacons = ref<Beacon[]>([])
const loaded = ref(false)
const search = ref('')

const formOpen = ref(false)
const formRoom = ref<Room | null>(null) // null = add

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

function openAdd() {
  formRoom.value = null
  formOpen.value = true
}
function openEdit(r: Room) {
  formRoom.value = r
  formOpen.value = true
}
function openDetail(r: Room) {
  router.push(`/rooms/${r.zoom_workspace_id}`)
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
            <tr v-for="r in filtered" :key="r.zoom_workspace_id" class="rowlink" @click="openDetail(r)">
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

    <RoomFormModal
      :open="formOpen"
      :room="formRoom"
      :beacon="formRoom ? (beaconByWs[formRoom.zoom_workspace_id] ?? null) : null"
      @close="formOpen = false"
      @saved="refresh"
    />
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
</style>
