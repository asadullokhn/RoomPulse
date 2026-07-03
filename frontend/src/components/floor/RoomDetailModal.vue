<script setup lang="ts">
import { ref, watch } from 'vue'
import { getEvents, getReservations } from '@/api/client'
import type { Room, OccupancyEntry, Reservation, EventEntry } from '@/api/types'

const props = defineProps<{
  open: boolean
  roomName: string | null
  room: Room | null
  occupancy: OccupancyEntry | null
}>()
const emit = defineEmits<{ close: [] }>()

const booking = ref<Reservation | null>(null)
const events = ref<EventEntry[]>([])
const loading = ref(false)

function fmtTime(s?: string) { return s ? new Date(s).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '—' }
function fmtAgoShort(s: number) {
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.floor(s / 60)}m`
  if (s < 86400) return `${Math.floor(s / 3600)}h`
  return `${Math.floor(s / 86400)}d`
}

watch(() => [props.open, props.room?.zoom_workspace_id], async ([open]) => {
  if (!open || !props.room) { booking.value = null; events.value = []; return }
  const ws = props.room.zoom_workspace_id
  loading.value = true
  try {
    const [ev, resv] = await Promise.all([getEvents(ws, 25), getReservations()])
    if (!props.open || props.room?.zoom_workspace_id !== ws) return // closed/switched while fetching
    events.value = ev
    booking.value = resv.find(v => v.zoom_workspace_id === ws) ?? null
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div class="modal" v-if="open" role="presentation">
    <div class="modal-bg" @click="emit('close')" />
    <div class="sheet" role="dialog" aria-modal="true" aria-labelledby="m-name">
      <button class="sheet-x" aria-label="Close" @click="emit('close')">✕</button>
      <div class="sheet-head">
        <div>
          <h2 id="m-name">{{ roomName }}</h2>
          <div class="sheet-sub">{{ [room?.has_tv ? 'Zoom Room' : 'Reservation-only', room?.capacity ? `Capacity ${room.capacity}` : ''].filter(Boolean).join('  ·  ') }}</div>
        </div>
        <span v-if="(occupancy?.count ?? 0) > 0" class="pill"><svg viewBox="0 0 24 24" fill="currentColor"><circle cx="12" cy="8" r="3.4"/><path d="M5.5 20c0-3.6 2.9-6 6.5-6s6.5 2.4 6.5 6z"/></svg>{{ occupancy?.count }}</span>
        <span v-else class="ghost">Open</span>
      </div>
      <div class="m-sec">
        <div class="m-h">Inside now</div>
        <div v-if="(occupancy?.count ?? 0) > 0">
          <span v-for="u in occupancy?.users ?? []" :key="u" class="who-chip">
            <svg viewBox="0 0 24 24" fill="currentColor"><circle cx="12" cy="8" r="3.4"/><path d="M5.5 20c0-3.6 2.9-6 6.5-6s6.5 2.4 6.5 6z"/></svg>{{ u }}
          </span>
        </div>
        <div v-else class="m-empty">No one here right now.</div>
      </div>
      <div class="m-sec">
        <div class="m-h">Booking</div>
        <div v-if="!room" class="m-empty">—</div>
        <div v-else-if="booking">
          <div class="m-line"><strong>{{ booking.user_email || booking.user_id || '—' }}</strong></div>
          <div class="m-sub2">{{ fmtTime(booking.start_time) }}–{{ fmtTime(booking.end_time) }} · {{ booking.check_in_status.replace(/_/g, ' ') }}</div>
        </div>
        <div v-else class="m-empty">No booking today.</div>
      </div>
      <div class="m-sec">
        <div class="m-h">Recent activity</div>
        <div v-if="events.length">
          <div v-for="(e, i) in events" :key="i" class="act" :class="e.kind === 'enter' ? 'enter' : 'leave'">
            <span class="dot" />
            <span>{{ e.name || e.actor }} {{ e.kind === 'enter' ? 'entered' : 'left' }}</span>
            <span class="ago">{{ fmtAgoShort(e.ago_sec) }} ago</span>
          </div>
        </div>
        <div v-else class="m-empty">No activity recorded yet.</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.modal { position: fixed; inset: 0; z-index: 50; display: flex; align-items: center; justify-content: center; padding: 20px; }
.modal-bg { position: absolute; inset: 0; background: rgba(6,9,18,.62); backdrop-filter: blur(6px); -webkit-backdrop-filter: blur(6px); }
.sheet { position: relative; width: 100%; max-width: 430px; max-height: 84vh; overflow: auto;
  background: linear-gradient(180deg, #16203C, #111A33); border: 1px solid var(--line);
  border-radius: 18px; padding: 20px; box-shadow: 0 24px 60px rgba(0,0,0,.5); animation: sheetIn .18s ease; }
@keyframes sheetIn { from { opacity: 0; transform: translateY(10px) scale(.985); } to { opacity: 1; transform: none; } }
.sheet-x { position: absolute; top: 13px; right: 13px; width: 30px; height: 30px; border-radius: 50%;
  border: 1px solid var(--line); background: rgba(255,255,255,.04); color: var(--muted); font-size: 17px; line-height: 1; cursor: pointer; }
.sheet-x:hover { color: #fff; border-color: var(--accent); }
.sheet-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; padding-right: 34px; }
.sheet-head h2 { font-family: var(--f-display); font-size: 21px; margin: 0; }
.sheet-sub { font-size: 12px; color: var(--muted); margin-top: 3px; font-family: var(--f-mono); }
.m-sec { margin-top: 18px; }
.m-h { font-family: var(--f-mono); font-size: 10.5px; text-transform: uppercase; letter-spacing: 1.5px; color: var(--muted); margin-bottom: 9px; }
.who-chip { display: inline-flex; align-items: center; gap: 6px; background: rgba(47,230,176,.12);
  border: 1px solid var(--signal-line); color: #DFF6EE; border-radius: 999px; padding: 4px 11px; font-size: 13px; margin: 0 6px 7px 0; }
.who-chip svg { width: 13px; height: 13px; }
.m-empty { color: var(--faint); font-size: 13px; }
.m-line strong { font-weight: 600; }
.m-sub2 { font-size: 12px; color: var(--muted); margin-top: 2px; font-family: var(--f-mono); }
.act { display: flex; align-items: center; gap: 10px; padding: 8px 0; border-bottom: 1px solid rgba(150,170,220,.07); font-size: 13px; }
.act:last-child { border-bottom: 0; }
.act .dot { width: 7px; height: 7px; border-radius: 50%; flex: none; }
.act.enter .dot { background: var(--signal); box-shadow: 0 0 7px var(--signal); }
.act.leave .dot { background: rgba(176,190,224,.7); }
.act .ago { margin-left: auto; color: var(--faint); font-family: var(--f-mono); font-size: 11.5px; }
.pill { display: inline-flex; align-items: center; gap: 5px; font-family: var(--f-mono); font-weight: 600;
  font-size: 15px; background: var(--signal); color: #06231B; padding: 3px 11px; border-radius: 999px; }
.pill svg { width: 14px; height: 14px; }
.ghost { font-family: var(--f-mono); font-size: 13px; color: var(--open-text);
  padding: 2px 10px; border: 1px dashed rgba(176,190,224,.5); border-radius: 999px; }
@media (max-width: 560px) { .modal { padding: 12px; align-items: flex-end; } .sheet { max-height: 88vh; } }
</style>
