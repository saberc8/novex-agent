<script setup lang="ts">
defineProps<{ title: string, loading?: boolean, error?: string, success?: string, refreshable?: boolean }>()
defineEmits<{ 'refresh': [], 'dismiss-error': [], 'dismiss-success': [] }>()
</script>

<template>
  <div class="gi_page novex-console">
    <a-card :bordered="false" class="nx-workspace">
      <div class="nx-page-header">
        <h2>{{ title }}</h2>
        <a-space wrap>
          <slot name="actions" />
          <a-button v-if="refreshable !== false" :loading="loading" @click="$emit('refresh')"><template #icon><icon-refresh /></template>刷新</a-button>
        </a-space>
      </div>
      <a-alert v-if="error" type="error" closable class="nx-alert" @close="$emit('dismiss-error')">{{ error }}</a-alert>
      <a-alert v-if="success" type="success" closable class="nx-alert" @close="$emit('dismiss-success')">{{ success }}</a-alert>
      <slot />
    </a-card>
  </div>
</template>

<style scoped src="../novex.css"></style>
