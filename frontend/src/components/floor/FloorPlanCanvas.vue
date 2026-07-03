<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import type { FloorData, FloorRoom } from '@/api/types'

const props = defineProps<{
  floorData: FloorData | null
  occupancyByName: Record<string, number>
}>()
const emit = defineEmits<{ roomClick: [name: string] }>()

const NAT_W = 2489, NAT_H = 1380
// Bound dynamically (not a static `src="..."`) so Vue's asset-URL transform
// doesn't try to resolve it as a local file — it's a backend API route.
const floorImageSrc = '/floor/image'

const vp = ref<HTMLElement | null>(null)
const stage = ref<HTMLElement | null>(null)
const transform = ref('')

function norm(s: string) { return String(s || '').toLowerCase().replace(/\s+/g, ' ').trim() }

const vb = computed(() => props.floorData?.view_box ?? { x: 1.9, y: 153.0, w: 1209.3, h: 682.0 })
const rooms = computed<FloorRoom[]>(() => props.floorData?.rooms ?? [])

function centroid(pts: number[][]): [number, number] {
  let x = 0, y = 0
  for (const p of pts) { x += p[0]; y += p[1] }
  return [x / pts.length, y / pts.length]
}

const cells = computed(() => rooms.value.map(rm => {
  const [cx, cy] = centroid(rm.points)
  const left = ((cx - vb.value.x) / vb.value.w) * NAT_W
  const top = ((cy - vb.value.y) / vb.value.h) * NAT_H
  const count = props.occupancyByName[norm(rm.name)] ?? 0
  return { room: rm, points: rm.points.map(p => p.join(',')).join(' '), left, top, count, busy: count > 0 }
}))

const busyCount = computed(() => cells.value.filter(c => c.busy).length)

function fit() {
  if (!vp.value || !stage.value) return
  const r = vp.value.getBoundingClientRect()
  const pad = r.width < 560 ? 12 : 28
  const scale = Math.min((r.width - pad * 2) / NAT_W, (r.height - pad * 2) / NAT_H)
  const x = (r.width - NAT_W * scale) / 2
  const freeY = r.height - NAT_H * scale
  const y = freeY > 0 ? Math.min(freeY / 2, r.height * 0.1 + pad) : freeY / 2
  transform.value = `translate(${x}px,${y}px) scale(${scale})`
}

watch(() => props.floorData, () => nextTick(fit))
onMounted(() => { window.addEventListener('resize', fit); fit() })
onUnmounted(() => window.removeEventListener('resize', fit))
</script>

<template>
  <div class="viewport" ref="vp">
    <div class="stage" ref="stage" :style="{ transform }">
      <img class="floorimg" :src="floorImageSrc" alt="Floor plan of Apple Developer Academy Bali" draggable="false" />
      <div class="scrim" />
      <svg class="overlay" :viewBox="`${vb.x} ${vb.y} ${vb.w} ${vb.h}`" preserveAspectRatio="none" aria-hidden="true">
        <polygon v-for="c in cells" :key="c.room.name" :points="c.points" :class="{ busy: c.busy }"
          role="img" :aria-label="`${c.room.name}, ${c.busy ? c.count + (c.count === 1 ? ' person' : ' people') : 'available'}`"
          @click="emit('roomClick', c.room.name)" />
      </svg>
      <div class="labels" aria-live="polite" aria-label="Rooms">
        <div v-for="c in cells" :key="c.room.name" class="lbl" :class="{ busy: c.busy, sm: c.room.kind !== 'room' }"
          :style="{ left: c.left + 'px', top: c.top + 'px' }" tabindex="0" role="button"
          :aria-label="`${c.room.name} — details`"
          @click="emit('roomClick', c.room.name)"
          @keydown.enter.prevent="emit('roomClick', c.room.name)"
          @keydown.space.prevent="emit('roomClick', c.room.name)">
          <span class="ico">
            <svg v-if="c.room.kind === 'room'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="12" rx="1.5"/><path d="M8 20h8M12 16v4"/></svg>
            <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="7" width="18" height="13" rx="2"/><path d="M8 7V5a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
          </span>
          <span class="nm">{{ c.room.name }}</span>
          <span class="chip-slot">
            <span v-if="c.busy" class="pill"><svg viewBox="0 0 24 24" fill="currentColor"><circle cx="12" cy="8" r="3.4"/><path d="M5.5 20c0-3.6 2.9-6 6.5-6s6.5 2.4 6.5 6z"/></svg>{{ c.count }}</span>
            <span v-else class="ghost">Open</span>
          </span>
        </div>
      </div>
    </div>
    <div class="legend" aria-label="Legend">
      <span class="lg-item"><span class="sw sw-busy" />Occupied <em>{{ busyCount }}</em></span>
      <span class="lg-item"><span class="sw sw-open" />Open <em>{{ cells.length - busyCount }}</em></span>
    </div>
  </div>
</template>

<style scoped>
.legend { position: absolute; left: 16px; bottom: 16px; z-index: 4;
  display: flex; gap: 14px; align-items: center; font-size: 12px; color: var(--muted);
  background: rgba(17,26,51,.82); border: 1px solid var(--line); border-radius: 12px;
  padding: 9px 13px; backdrop-filter: blur(10px); -webkit-backdrop-filter: blur(10px); }
.lg-item { display: inline-flex; align-items: center; gap: 7px; }
.lg-item em { font-style: normal; font-family: var(--f-mono); color: var(--text); }
.sw { width: 22px; height: 14px; border-radius: 4px; display: inline-block; }
.sw-busy { background: var(--signal-dim); border: 2px solid var(--signal); box-shadow: 0 0 7px rgba(47,230,176,.5); }
.sw-open { background: transparent; border: 1.5px dashed rgba(176,190,224,.6); }
.viewport { position: relative; flex: 1; overflow: hidden; background: var(--ink); min-height: 0; }
.stage { position: absolute; top: 0; left: 0; width: 2489px; height: 1380px; transform-origin: 0 0; }
.floorimg { display: block; width: 2489px; height: 1380px; user-select: none; -webkit-user-drag: none;
  filter: grayscale(.85) brightness(.5) contrast(1.1); }
.scrim { position: absolute; inset: 0; pointer-events: none;
  background: radial-gradient(120% 90% at 50% 42%, rgba(10,15,31,.40), rgba(10,15,31,.66)); }
.overlay { position: absolute; inset: 0; width: 100%; height: 100%; pointer-events: none; }
.overlay polygon { fill: var(--open-fill); stroke: var(--open-line); stroke-width: 1.5;
  stroke-dasharray: 7 5; vector-effect: non-scaling-stroke; pointer-events: all; cursor: pointer;
  transition: fill .18s ease, stroke .18s ease; }
.overlay polygon:hover { fill: rgba(176,190,224,.20); }
.overlay polygon.busy:hover { fill: rgba(47,230,176,.42); }
.overlay polygon.busy { fill: var(--signal-dim); stroke: var(--signal-line); stroke-width: 2.5;
  stroke-dasharray: none; filter: drop-shadow(0 0 7px rgba(47,230,176,.55));
  animation: liveGlow 2.8s ease-in-out infinite; }
@keyframes liveGlow { 0%,100% { filter: drop-shadow(0 0 5px rgba(47,230,176,.40)); } 50% { filter: drop-shadow(0 0 11px rgba(47,230,176,.70)); } }
.labels { position: absolute; inset: 0; pointer-events: none; }
.lbl { position: absolute; transform: translate(-50%,-50%); display: flex; flex-direction: column;
  align-items: center; gap: 5px; text-align: center; max-width: 172px; pointer-events: auto; cursor: pointer;
  background: rgba(8,12,24,.46); backdrop-filter: blur(5px); -webkit-backdrop-filter: blur(5px);
  border-radius: 11px; padding: 6px 10px; box-shadow: 0 2px 8px rgba(7,11,22,.45); transition: background .15s; }
.lbl:hover { background: rgba(8,12,24,.68); }
.lbl .ico { width: 25px; height: 25px; opacity: .9; color: var(--open-text); }
.lbl .ico svg { width: 100%; height: 100%; }
.lbl .nm { font-family: var(--f-display); font-weight: 600; font-size: 20px; line-height: 1.12; color: var(--open-text); }
.lbl.busy .ico, .lbl.busy .nm { color: #fff; }
.lbl.sm { padding: 5px 8px; max-width: 130px; }
.lbl.sm .nm { font-size: 15px; }
.lbl.sm .ico { width: 18px; height: 18px; }
.pill { display: inline-flex; align-items: center; gap: 5px; font-family: var(--f-mono); font-weight: 600;
  font-size: 15px; font-variant-numeric: tabular-nums; background: var(--signal); color: #06231B;
  padding: 3px 11px; border-radius: 999px; box-shadow: 0 0 12px rgba(47,230,176,.55); }
.pill svg { width: 14px; height: 14px; }
.ghost { font-family: var(--f-mono); font-size: 13px; color: var(--open-text);
  padding: 2px 10px; border: 1px dashed rgba(176,190,224,.5); border-radius: 999px; }
.lbl.sm .pill { font-size: 13px; padding: 2px 9px; }
.lbl.sm .ghost { font-size: 11px; }
@media (max-width: 560px) {
  .legend { font-size: 11px; gap: 10px; padding: 6px 11px; left: 12px; bottom: 12px; }
}
</style>
