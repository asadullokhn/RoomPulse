<script setup lang="ts">
import type { Room, OccupancyEntry } from '@/api/types'

const props = defineProps<{ rooms: Room[]; occupancyByWs: Record<string, OccupancyEntry> }>()

function occCount(ws: string) { return props.occupancyByWs[ws]?.count ?? 0 }
function occUsers(ws: string) { return props.occupancyByWs[ws]?.users?.join(', ') ?? '' }
</script>

<template>
  <div class="rooms">
    <div v-for="rm in rooms" :key="rm.zoom_workspace_id" class="room" :class="{ busy: occCount(rm.zoom_workspace_id) > 0 }">
      <div class="rn">{{ rm.name }} <span class="dot" :class="{ on: occCount(rm.zoom_workspace_id) > 0 }" /></div>
      <div class="head"><span class="c">{{ occCount(rm.zoom_workspace_id) }}</span><span class="cap">/ {{ rm.capacity }} seats</span></div>
      <div class="who">{{ occUsers(rm.zoom_workspace_id) || 'empty' }}</div>
    </div>
  </div>
</template>

<style scoped>
.rooms { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 12px; }
.room { background: linear-gradient(180deg, var(--panel-2), var(--panel)); border: 1px solid var(--line);
  border-radius: var(--r); padding: 14px 15px; }
.room.busy { border-color: var(--signal-line); }
.rn { font-weight: 600; display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.head { display: flex; align-items: baseline; gap: 8px; margin-top: 10px; }
.head .c { font-family: var(--f-display); font-size: 26px; font-weight: 700; line-height: 1; }
.room.busy .head .c { color: var(--signal); }
.head .cap { font-size: 12px; color: var(--muted); }
.who { margin-top: 9px; font-size: 12px; color: var(--muted); min-height: 16px; }
.dot { width: 7px; height: 7px; border-radius: 50%; background: var(--faint); display: inline-block; }
.dot.on { background: var(--signal); box-shadow: 0 0 7px var(--signal); }
</style>
