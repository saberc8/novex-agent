<script setup lang="ts">
import { onActivated, onBeforeUnmount, onDeactivated, onMounted, ref } from 'vue'
import { novex } from '@/apis/novex'
import NovexPage from '../shared/NovexPage.vue'

defineOptions({ name: 'NovexChat' })
const chatFrame = ref<HTMLIFrameElement | null>(null)
const chatStatus = ref('正在建立安全连接…')
const chatBase
  = import.meta.env.VITE_NOVEX_CHAT_URL
    || `${location.protocol}//${location.hostname}:8312`
const chatOrigin = new URL(chatBase).origin
const chatUrl = `${chatBase}/?embed=1&parentOrigin=${encodeURIComponent(location.origin)}`
async function handleChatMessage(event: MessageEvent) {
  if (
    event.origin !== chatOrigin
    || event.source !== chatFrame.value?.contentWindow
  ) {
    return
  }
  if (event.data?.type === 'novex:authenticated') {
    chatStatus.value = '已安全登录'
    return
  }
  if (event.data?.type !== 'novex:ready') return
  try {
    const result = await novex<{
      ticket: string
    }>('/embed-tickets', 'POST', { origin: location.origin })
    chatFrame.value?.contentWindow?.postMessage(
      { type: 'novex:launch', ticket: result.ticket },
      chatOrigin,
    )
  } catch (e) {
    chatStatus.value
      = e instanceof Error ? e.message : '自动登录失败，可以在对话页面登录'
  }
}
function listen() {
  window.addEventListener('message', handleChatMessage)
}
function unlisten() {
  window.removeEventListener('message', handleChatMessage)
}
onMounted(listen)
onActivated(listen)
onDeactivated(unlisten)
onBeforeUnmount(unlisten)
</script>

<template>
  <NovexPage title="对话助手" :refreshable="false">
    <div class="nx-section-heading"><a-badge status="processing" :text="chatStatus" /><a-link :href="chatBase" target="_blank" rel="noopener">在独立窗口打开 <icon-launch /></a-link></div>
    <iframe ref="chatFrame" :src="chatUrl" title="Novex 独立对话助手" class="nx-chat-frame" allow="clipboard-write" />
  </NovexPage>
</template>

<style scoped src="../novex.css"></style>
