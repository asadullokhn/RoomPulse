<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ total: number; page: number; perPage: number }>()
const emit = defineEmits<{ 'update:page': [value: number] }>()

const pages = computed(() => Math.max(1, Math.ceil(props.total / props.perPage)))
const from = computed(() => (props.page - 1) * props.perPage + 1)
const to = computed(() => Math.min(props.page * props.perPage, props.total))
</script>

<template>
  <div v-if="total > perPage" class="pager">
    <span class="range">{{ from }}&#8211;{{ to }} of {{ total }}</span>
    <button class="btn-secondary" :disabled="page <= 1" aria-label="Previous page" @click="emit('update:page', page - 1)">&#8249;</button>
    <button class="btn-secondary" :disabled="page >= pages" aria-label="Next page" @click="emit('update:page', page + 1)">&#8250;</button>
  </div>
</template>

<style scoped>
.pager { display: flex; align-items: center; justify-content: flex-end; gap: 8px; padding: 12px 4px 2px; }
.range { font-size: 12px; color: var(--muted); font-variant-numeric: tabular-nums; margin-right: 4px; }
button { min-width: 30px; padding: 4px 10px; }
</style>
