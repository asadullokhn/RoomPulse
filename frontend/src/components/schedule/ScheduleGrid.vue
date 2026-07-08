<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Room, Reservation } from '@/api/types'

const DAY_START_H = 7
const WINDOW_MIN = 720 // 07:00-19:00

const props = defineProps<{ rooms: Room[]; reservations: Reservation[]; date: Date }>()
const emit = defineEmits<{
  select: [reservation: Reservation]
  create: [slot: { workspaceId: string; start: Date; end: Date }]
}>()

const nowTick = ref(Date.now())
setInterval(() => { nowTick.value = Date.now() }, 30_000)

const dayStart = computed(() => {
  const d = new Date(props.date)
  d.setHours(DAY_START_H, 0, 0, 0)
  return d.getTime()
})
const dayEnd = computed(() => dayStart.value + WINDOW_MIN * 60_000)

const hours = Array.from({ length: 13 }, (_, i) => DAY_START_H + i)

function pct(t: number): number {
  return Math.min(100, Math.max(0, ((t - dayStart.value) / 60_000) / WINDOW_MIN * 100))
}

interface Block {
  r: Reservation
  left: number
  width: number
  tone: string
  faded: boolean
}

const blocksByRoom = computed<Record<string, Block[]>>(() => {
  const m: Record<string, Block[]> = {}
  for (const r of props.reservations) {
    const s = Date.parse(r.start_time), e = Date.parse(r.end_time)
    if (e <= dayStart.value || s >= dayEnd.value) continue
    const left = pct(s)
    const width = Math.max(pct(e) - left, 1.2)
    const tone = r.check_in_status === 'checked_in' ? 'green'
      : r.status === 'booked' ? 'blue' : 'gray'
    const faded = r.status !== 'booked'
    ;(m[r.zoom_workspace_id] ??= []).push({ r, left, width, tone, faded })
  }
  return m
})

const nowLine = computed(() => {
  const now = nowTick.value
  if (now < dayStart.value || now > dayEnd.value) return null
  return pct(now)
})

function bookerShort(r: Reservation): string {
  const who = r.user_email || r.booked_by_user_id || r.user_id || ''
  return who.split('@')[0] || 'booked'
}

function timeRange(r: Reservation): string {
  const f = (s: string) => new Date(s).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  return `${f(r.start_time)}–${f(r.end_time)}`
}

function onTrackClick(e: MouseEvent, ws: string) {
  const track = e.currentTarget as HTMLElement
  const rect = track.getBoundingClientRect()
  const minutes = Math.floor(((e.clientX - rect.left) / rect.width) * WINDOW_MIN / 30) * 30
  const start = new Date(dayStart.value + minutes * 60_000)
  const end = new Date(Math.min(start.getTime() + 60 * 60_000, dayEnd.value))
  emit('create', { workspaceId: ws, start, end })
}
</script>

<template>
  <div class="card grid-card">
    <div class="scroll">
      <div class="grid">
        <div class="axis">
          <div class="corner" />
          <div class="track-head">
            <span v-for="h in hours" :key="h" class="hour" :style="{ left: `${((h - 7) / 12) * 100}%` }">
              {{ h }}:00
            </span>
          </div>
        </div>

        <div v-for="rm in rooms" :key="rm.zoom_workspace_id" class="row">
          <div class="room">
            <div class="rn">{{ rm.name }}</div>
            <div class="cap">{{ rm.capacity }} seats</div>
          </div>
          <div class="track" @click="onTrackClick($event, rm.zoom_workspace_id)">
            <span v-for="h in hours.slice(1, -1)" :key="h" class="gridline" :style="{ left: `${((h - 7) / 12) * 100}%` }" />
            <button
              v-for="b in blocksByRoom[rm.zoom_workspace_id] ?? []"
              :key="b.r.reservation_id"
              class="block"
              :class="[b.tone, { faded: b.faded }]"
              :style="{ left: `${b.left}%`, width: `${b.width}%` }"
              :title="`${bookerShort(b.r)} ${timeRange(b.r)} (${b.r.status})`"
              @click.stop="emit('select', b.r)"
            >
              <span class="who">{{ bookerShort(b.r) }}</span>
              <span class="when">{{ timeRange(b.r) }}</span>
            </button>
            <span v-if="nowLine !== null" class="now" :style="{ left: `${nowLine}%` }"><i /></span>
          </div>
        </div>

        <div v-if="!rooms.length" class="empty">No rooms yet.</div>
      </div>
    </div>
    <div class="legend">
      <span><i class="sw blue" />Booked</span>
      <span><i class="sw green" />Checked-In</span>
      <span><i class="sw gray" />Released or cancelled</span>
      <span class="hint">Click an empty slot to book it.</span>
    </div>
  </div>
</template>

<style scoped>
.grid-card { overflow: visible; }
.grid { min-width: 860px; padding: 0 0 6px; }
.axis { display: grid; grid-template-columns: 168px 1fr; }
.corner { border-bottom: 1px solid var(--line-soft); }
.track-head { position: relative; height: 30px; border-bottom: 1px solid var(--line-soft); }
.hour { position: absolute; top: 8px; transform: translateX(-50%); font-size: 10.5px; color: var(--faint);
  font-variant-numeric: tabular-nums; }
.row { display: grid; grid-template-columns: 168px 1fr; border-bottom: 1px solid var(--line-soft); }
.row:last-of-type { border-bottom: 0; }
.room { padding: 8px 14px; border-right: 1px solid var(--line-soft); }
.rn { font-size: 12.5px; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.cap { font-size: 10.5px; color: var(--faint); }
.track { position: relative; height: 46px; cursor: copy; }
.gridline { position: absolute; top: 0; bottom: 0; width: 1px; background: var(--line-soft); }
.block { position: absolute; top: 5px; bottom: 5px; border: none; border-radius: 6px; cursor: pointer;
  display: flex; flex-direction: column; justify-content: center; gap: 0; padding: 2px 7px; overflow: hidden;
  text-align: left; font-family: var(--f-body); transition: filter .16s ease; }
.block:hover { filter: brightness(.96); z-index: 3; }
.block .who { font-size: 11px; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.block .when { font-size: 9.5px; opacity: .75; white-space: nowrap; overflow: hidden; font-variant-numeric: tabular-nums; }
.block.blue { background: rgba(0, 113, 227, .14); box-shadow: inset 3px 0 0 var(--accent); color: #0058b0; }
.block.green { background: rgba(52, 199, 89, .16); box-shadow: inset 3px 0 0 var(--signal); color: #1d8a3e; }
.block.gray { background: rgba(0, 0, 0, .06); box-shadow: inset 3px 0 0 rgba(0, 0, 0, .22); color: var(--muted); }
.block.faded { opacity: .55; z-index: 1; }
.now { position: absolute; top: 0; bottom: 0; width: 1.5px; background: var(--danger); z-index: 4; pointer-events: none; }
.now i { position: absolute; top: -2px; left: -2.75px; width: 7px; height: 7px; border-radius: 50%; background: var(--danger); }
.legend { display: flex; align-items: center; gap: 16px; flex-wrap: wrap; padding: 10px 16px;
  border-top: 1px solid var(--line-soft); font-size: 11.5px; color: var(--muted); }
.legend span { display: inline-flex; align-items: center; gap: 6px; }
.sw { width: 10px; height: 10px; border-radius: 3px; display: inline-block; }
.sw.blue { background: rgba(0, 113, 227, .35); }
.sw.green { background: rgba(52, 199, 89, .45); }
.sw.gray { background: rgba(0, 0, 0, .18); }
.legend .hint { margin-left: auto; color: var(--faint); }
</style>
