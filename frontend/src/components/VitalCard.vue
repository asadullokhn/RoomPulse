<script setup lang="ts">
withDefaults(defineProps<{
  label: string
  value: string | number
  sub?: string
  tone?: 'default' | 'hero' | 'good' | 'warn' | 'bad'
}>(), { tone: 'default' })
</script>

<template>
  <div class="vital" :class="tone !== 'default' ? tone : ''">
    <div class="k">{{ label }}</div>
    <div class="v">{{ value }}</div>
    <div class="sub" v-if="sub || $slots.sub"><slot name="sub">{{ sub }}</slot></div>
  </div>
</template>
