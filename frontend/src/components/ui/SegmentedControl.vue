<script setup lang="ts">
defineProps<{ options: { value: string; label: string }[]; modelValue: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
</script>

<template>
  <div class="seg" role="tablist">
    <button
      v-for="o in options"
      :key="o.value"
      role="tab"
      :aria-selected="modelValue === o.value"
      :class="{ on: modelValue === o.value }"
      @click="emit('update:modelValue', o.value)"
    >{{ o.label }}</button>
  </div>
</template>

<style scoped>
.seg { display: inline-flex; background: #ebebf0; border-radius: 9px; padding: 2px; gap: 2px; }
button { border: none; background: none; padding: 5px 13px; border-radius: 7px;
  font-size: 12.5px; font-weight: 500; font-family: var(--f-body); color: var(--muted);
  cursor: pointer; transition: color .16s ease; white-space: nowrap; }
button.on { background: var(--panel); color: var(--text);
  box-shadow: 0 1px 2.5px rgba(0, 0, 0, .12), 0 0 0 0.5px rgba(0, 0, 0, .04); }
</style>
