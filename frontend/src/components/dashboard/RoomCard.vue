<script setup lang="ts">
import { computed } from 'vue'
import type { Room, Reservation } from '@/api/types'

const props = defineProps<{
  room: Room
  count: number
  users: string[]
  booked?: Reservation
}>()

const state = computed<'busy' | 'booked' | 'empty'>(() =>
  props.count > 0 ? 'busy' : props.booked ? 'booked' : 'empty')

const cap = computed(() => props.room.capacity ? `${props.count}/${props.room.capacity}` : `${props.count}`)
const loc = computed(() => [props.room.floor, props.room.has_tv ? 'Zoom Room' : null].filter(Boolean).join(' · ') || 'Reservation-only')

function fmtTime(s?: string) {
  if (!s) return '—'
  return new Date(s).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}
</script>

<template>
  <div class="room" :class="state">
    <div class="top">
      <div><div class="name">{{ room.name }}</div><div class="loc">{{ loc }}</div></div>
      <span class="cap">{{ cap }}</span>
    </div>
    <template v-if="state === 'busy'">
      <div class="state">
        <span class="ico"><svg viewBox="0 0 24 24" fill="currentColor"><circle cx="12" cy="8" r="3.4"/><path d="M5.5 20c0-3.6 2.9-6 6.5-6s6.5 2.4 6.5 6z"/></svg></span>
        In use
        <span class="pill"><svg viewBox="0 0 24 24" fill="currentColor"><circle cx="12" cy="8" r="3.4"/><path d="M5.5 20c0-3.6 2.9-6 6.5-6s6.5 2.4 6.5 6z"/></svg>{{ count }}</span>
      </div>
      <div class="who">{{ users.join(', ') }}</div>
    </template>
    <template v-else-if="state === 'booked'">
      <div class="state">
        <span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 7.5V12l3 1.8"/></svg></span>
        Booked
      </div>
      <div class="booked-t">{{ fmtTime(booked?.start_time) }}–{{ fmtTime(booked?.end_time) }} · awaiting check-in</div>
    </template>
    <template v-else>
      <div class="state">
        <span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M8.5 12.5l2.4 2.4 4.6-5"/></svg></span>
        Available
      </div>
    </template>
  </div>
</template>

<style scoped>
.room { position: relative; background: linear-gradient(180deg, var(--panel-2), var(--panel));
  border: 1px solid var(--line); border-radius: var(--r); padding: 16px 17px 14px; }
.room.busy { border-color: var(--signal-line); }
.room.booked { border-left: 2px solid var(--amber); }
.room.empty { opacity: .72; border-left: 2px solid var(--open-line); }
.room.busy::after { content: ""; position: absolute; inset: -1px; border-radius: inherit; pointer-events: none;
  border: 1px solid var(--signal); opacity: 0; animation: pulse 2.8s ease-out infinite; }
@keyframes pulse { 0% { opacity: .55; } 70%,100% { opacity: 0; } }
.top { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; }
.name { font-family: var(--f-display); font-weight: 600; font-size: 16px; }
.loc { font-size: 11.5px; color: var(--faint); margin-top: 2px; }
.cap { font-family: var(--f-mono); font-size: 12px; color: var(--muted); white-space: nowrap; }
.state { display: flex; align-items: center; gap: 7px; margin: 13px 0 4px; font-size: 13px; font-weight: 600; }
.state .ico { width: 15px; height: 15px; flex: none; display: inline-flex; }
.state .ico svg { width: 100%; height: 100%; }
.room.busy .state { color: var(--signal); }
.room.booked .state { color: var(--amber); }
.room.empty .state { color: var(--muted); }
.pill { display: inline-flex; align-items: center; gap: 4px; margin-left: auto; font-family: var(--f-mono);
  font-weight: 600; font-size: 13px; font-variant-numeric: tabular-nums; background: var(--signal); color: #06231B;
  padding: 2px 10px; border-radius: 999px; box-shadow: 0 0 10px rgba(47,230,176,.45); }
.pill svg { width: 13px; height: 13px; }
.who { font-size: 12.5px; color: var(--text); min-height: 17px; }
.booked-t { font-size: 12px; color: var(--amber); margin-top: 2px; }
</style>
