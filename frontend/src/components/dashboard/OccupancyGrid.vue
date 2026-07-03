<script setup lang="ts">
import { computed } from 'vue'
import type { Room, Reservation, OccupancyEntry } from '@/api/types'
import RoomCard from './RoomCard.vue'

const props = defineProps<{
  rooms: Room[]
  occupancyByWs: Record<string, OccupancyEntry>
  reservations: Reservation[]
}>()

const cards = computed(() => {
  const list = props.rooms.map(r => {
    const ws = r.zoom_workspace_id
    const o = props.occupancyByWs[ws]
    const count = o?.count ?? 0
    const booked = props.reservations.find(v => v.zoom_workspace_id === ws && v.check_in_status === 'not_checked_in')
    const rank = count > 0 ? 0 : booked ? 1 : 2
    return { room: r, count, users: o?.users ?? [], booked, rank }
  })
  return list.sort((a, b) => a.rank - b.rank || b.count - a.count || a.room.name.localeCompare(b.room.name))
})
</script>

<template>
  <div class="grid" v-if="cards.length">
    <RoomCard v-for="c in cards" :key="c.room.zoom_workspace_id" :room="c.room" :count="c.count" :users="c.users" :booked="c.booked" />
  </div>
  <div class="card" v-else><div class="empty"><b>No rooms yet</b>Sync from Zoom to load your workspaces.</div></div>
</template>

<style scoped>
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(258px, 1fr)); gap: 14px; }
</style>
