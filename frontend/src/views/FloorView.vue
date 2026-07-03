<script setup lang="ts">
import { ref, computed } from 'vue'
import AppHeader from '@/components/AppHeader.vue'
import FloorPlanCanvas from '@/components/floor/FloorPlanCanvas.vue'
import RoomDetailModal from '@/components/floor/RoomDetailModal.vue'
import { usePoll } from '@/composables/usePoll'
import { useConnection } from '@/composables/useConnection'
import { getFloorRooms, getRooms, getOccupancy } from '@/api/client'
import type { FloorData, Room, OccupancyEntry } from '@/api/types'

document.title = 'QuickRoom · Floor plan'

const { connected, markUp, markDown } = useConnection()
const floorData = ref<FloorData | null>(null)
const rooms = ref<Room[]>([])
const occupancy = ref<OccupancyEntry[]>([])
const openRoomName = ref<string | null>(null)

function norm(s: string) { return String(s || '').toLowerCase().replace(/\s+/g, ' ').trim() }

const occByWs = computed(() => {
  const m: Record<string, OccupancyEntry> = {}
  for (const o of occupancy.value) m[o.workspace_id] = o
  return m
})
const occupancyByName = computed(() => {
  const m: Record<string, number> = {}
  for (const r of rooms.value) m[norm(r.name)] = occByWs.value[r.zoom_workspace_id]?.count ?? 0
  return m
})
const openRoom = computed(() => rooms.value.find(r => norm(r.name) === norm(openRoomName.value ?? '')) ?? null)
const openRoomOcc = computed(() => openRoom.value ? occByWs.value[openRoom.value.zoom_workspace_id] ?? null : null)

async function loadFloor() {
  try { floorData.value = await getFloorRooms() } catch { /* keep last-known layout */ }
}
async function refresh() {
  try {
    const [r, occ] = await Promise.all([getRooms(), getOccupancy()])
    rooms.value = r; occupancy.value = occ
    markUp()
  } catch {
    markDown()
  }
}
loadFloor()
usePoll(refresh, 3000)
</script>

<template>
  <div class="page">
    <AppHeader active="floor" :connected="connected" />
    <FloorPlanCanvas :floor-data="floorData" :occupancy-by-name="occupancyByName" @room-click="name => openRoomName = name" />
    <RoomDetailModal :open="openRoomName !== null" :room-name="openRoomName" :room="openRoom" :occupancy="openRoomOcc" @close="openRoomName = null" />
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; flex: 1; min-height: 0; background: var(--ink); }
</style>
