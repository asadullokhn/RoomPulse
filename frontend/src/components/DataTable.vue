<script setup lang="ts">
defineProps<{
  columns: string[]
  rows: unknown[]
  emptyTitle: string
  emptyBody: string
}>()
</script>

<template>
  <div class="card">
    <div class="scroll" v-if="rows.length">
      <table>
        <thead><tr><th v-for="c in columns" :key="c">{{ c }}</th></tr></thead>
        <tbody><slot /></tbody>
      </table>
    </div>
    <div class="empty" v-else><b>{{ emptyTitle }}</b>{{ emptyBody }}</div>
  </div>
</template>
