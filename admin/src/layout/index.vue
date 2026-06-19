<template>
  <LayoutMix v-if="appStore.layout === 'mix'"></LayoutMix>
  <LayoutDefault v-else></LayoutDefault>
</template>

<script setup lang="ts">
import { useAppStore } from '@/stores'
import LayoutDefault from './LayoutDefault.vue'
import LayoutMix from './LayoutMix.vue'

defineOptions({ name: 'Layout' })
const appStore = useAppStore()
</script>

<style scoped lang="scss"></style>
