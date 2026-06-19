<template>
  <span class="gi-dot" :class="[{ 'gi-dot-processing': animation }, `gi-dot-${type}`]"></span>
</template>

<script setup lang="ts">
import './dot.scss'

withDefaults(defineProps<{
  animation?: boolean
  type?: 'primary' | 'success' | 'warning' | 'danger' | 'info'
}>(), {
  animation: true,
  type: 'primary',
})
</script>
