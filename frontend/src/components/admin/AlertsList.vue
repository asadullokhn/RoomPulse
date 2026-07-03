<script setup lang="ts">
import type { Collision, Overstay } from '@/api/types'

defineProps<{ collisions: Collision[]; overstays: Overstay[] }>()

function fmtDur(sec: number) {
  sec = Math.max(0, sec | 0)
  if (sec < 60) return `${sec}s`
  const m = Math.round(sec / 60)
  if (m < 60) return `${m}m`
  return `${Math.floor(m / 60)}h ${m % 60}m`
}
</script>

<template>
  <div v-if="collisions.length || overstays.length" class="alerts">
    <div v-for="c in collisions" :key="'c' + c.reservation_id" class="alert">
      <div class="ic">!</div>
      <div>
        <div class="t">Room conflict — {{ c.room_name }}</div>
        <div class="d">Booked to <b>{{ c.booker }}</b>, but occupied by <b>{{ (c.occupants || []).join(', ') }}</b>. The booker never showed.</div>
      </div>
    </div>
    <div v-for="o in overstays" :key="'o' + o.reservation_id" class="alert over">
      <div class="ic">◷</div>
      <div>
        <div class="t">Overstay — {{ o.room_name }}</div>
        <div class="d">Booking for <b>{{ o.booker }}</b> ended <b>{{ fmtDur(o.over_by_sec) }} ago</b> but the room is still in use.</div>
      </div>
    </div>
  </div>
  <div v-else class="allclear"><b>All clear.</b> No conflicts or overstays right now.</div>
</template>

<style scoped>
.alerts { display: grid; gap: 12px; }
.alert { display: flex; gap: 13px; align-items: flex-start; padding: 14px 16px; border-radius: var(--r);
  border: 1px solid var(--danger-line); background: var(--danger-dim); }
.alert.over { border-color: var(--amber-line); background: var(--amber-dim); }
.alert .ic { width: 30px; height: 30px; border-radius: 8px; flex: none; display: grid; place-items: center;
  background: rgba(255,107,107,.18); color: var(--danger); font-weight: 700; font-family: var(--f-display); }
.alert.over .ic { background: rgba(244,183,64,.2); color: var(--amber); }
.alert .t { font-weight: 600; }
.alert .d { color: var(--muted); font-size: 13px; margin-top: 2px; }
.alert .d b { color: var(--text); font-weight: 600; }
.allclear { padding: 18px 16px; color: var(--muted); text-align: center; border: 1px dashed var(--line); border-radius: var(--r); }
.allclear b { color: var(--signal); }
</style>
