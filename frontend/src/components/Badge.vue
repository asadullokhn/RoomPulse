<script setup lang="ts">
defineProps<{ tone: 'signal' | 'amber' | 'muted' | 'danger' }>()
</script>

<template>
  <span class="badge" :class="`b-${tone}`"><slot /></span>
</template>
