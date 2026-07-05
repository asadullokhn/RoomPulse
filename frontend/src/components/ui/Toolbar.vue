<script setup lang="ts">
defineProps<{ search?: string; searchPlaceholder?: string }>()
const emit = defineEmits<{ 'update:search': [value: string] }>()
</script>

<template>
  <div class="toolbar">
    <div v-if="search !== undefined" class="search">
      <span class="glass" aria-hidden="true" />
      <input
        class="field"
        type="search"
        :placeholder="searchPlaceholder ?? 'Search'"
        :value="search"
        @input="emit('update:search', ($event.target as HTMLInputElement).value)"
      />
    </div>
    <div class="filters"><slot name="filters" /></div>
    <div class="actions"><slot name="actions" /></div>
  </div>
</template>

<style scoped>
.toolbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 12px; }
.search { position: relative; }
.search .field { padding-left: 30px; width: 230px; border-radius: 9px; }
.glass { position: absolute; left: 10px; top: 50%; width: 11px; height: 11px; margin-top: -7px;
  border: 1.5px solid var(--faint); border-radius: 50%; }
.glass::after { content: ""; position: absolute; width: 5px; height: 1.5px; background: var(--faint);
  bottom: -2px; right: -3px; transform: rotate(45deg); }
.filters { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.actions { margin-left: auto; display: flex; align-items: center; gap: 8px; }
@media (max-width: 640px) { .search .field { width: 100%; } }
</style>
