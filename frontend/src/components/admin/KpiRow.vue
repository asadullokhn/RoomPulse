<script setup lang="ts">
import { computed } from 'vue'
import VitalCard from '@/components/VitalCard.vue'
import type { Utilization } from '@/api/types'

const props = defineProps<{ util: Utilization }>()
const noShowPct = computed(() => Math.round((props.util.no_show_rate || 0) * 100))
const noShowTone = computed<'good' | 'warn' | 'bad'>(() => noShowPct.value >= 40 ? 'bad' : noShowPct.value >= 20 ? 'warn' : 'good')
</script>

<template>
  <section class="vitals">
    <VitalCard label="Bookings" :value="util.bookings" />
    <VitalCard label="Checked in" :value="util.checked_in" tone="good" />
    <VitalCard label="Reclaimed" :value="util.no_show_released" />
    <VitalCard label="No-show rate" :value="`${noShowPct}%`" :tone="noShowTone" />
    <VitalCard label="Rooms in use" :value="`${util.rooms_occupied}/${util.rooms_total}`" tone="good" />
    <VitalCard label="People present" :value="util.people_present" tone="good" />
  </section>
</template>

<style scoped>
.vitals { display: grid; grid-template-columns: repeat(6, 1fr); gap: 12px; margin-bottom: 34px; }
@media (max-width: 900px) { .vitals { grid-template-columns: repeat(3, 1fr); } }
@media (max-width: 540px) { .vitals { grid-template-columns: repeat(2, 1fr); } }
</style>
