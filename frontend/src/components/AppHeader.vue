<script setup lang="ts">
defineProps<{
  connected: boolean
}>()
</script>

<template>
  <header>
    <div class="brand">
      <span class="beacon"><i /></span>
      <div><h1>Quick<b>Room</b></h1><div class="tag">Admin · Apple Developer Academy · Bali</div></div>
    </div>
    <div class="right">
      <span class="chip" :class="connected ? 'live' : 'down'">
        <span class="led" /><span>{{ connected ? 'Live' : 'Reconnecting…' }}</span>
      </span>
    </div>
  </header>
</template>

<style scoped>
header {
  position: sticky; top: 0; z-index: 20;
  display: flex; align-items: center; gap: 16px; flex-wrap: wrap;
  padding: 13px 22px; border-bottom: 1px solid var(--line);
  background: rgba(10,15,31,.72); backdrop-filter: blur(14px); -webkit-backdrop-filter: blur(14px);
}
.brand { display: flex; align-items: center; gap: 11px; }
.beacon { position: relative; width: 11px; height: 11px; flex: none; }
.beacon i { position: absolute; inset: 0; margin: auto; width: 9px; height: 9px; border-radius: 50%;
  background: #2FE6B0; box-shadow: 0 0 12px #2FE6B0; }
.beacon::before, .beacon::after { content: ""; position: absolute; inset: -2px; border-radius: 50%;
  border: 1.5px solid #2FE6B0; opacity: 0; animation: ping 2.6s cubic-bezier(.2,.6,.3,1) infinite; }
.beacon::after { animation-delay: 1.3s; }
@keyframes ping { 0% { transform: scale(.7); opacity: .7; } 80%,100% { transform: scale(2.6); opacity: 0; } }
.brand h1 { font-family: var(--f-display); font-size: 18px; font-weight: 700; letter-spacing: .2px; margin: 0; }
.brand h1 b { color: #2FE6B0; }
.brand .tag { font-size: 11px; color: var(--muted); margin-top: 1px; letter-spacing: .2px; }
.right { margin-left: auto; display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.chip { font-family: var(--f-mono); font-size: 11px; color: var(--muted);
  border: 1px solid var(--line); border-radius: 999px; padding: 5px 11px; display: inline-flex; gap: 6px; align-items: center; }
.chip .led { width: 7px; height: 7px; border-radius: 50%; background: var(--faint); }
.chip.live .led { background: #2FE6B0; box-shadow: 0 0 8px #2FE6B0; }
.chip.down { color: var(--amber); border-color: rgba(244,183,64,.4); }
.chip.down .led { background: var(--amber); box-shadow: 0 0 8px var(--amber); }
@media (max-width: 560px) {
  header { padding: 12px 14px; }
  .brand .tag { display: none; }
}
</style>
