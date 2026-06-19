<template>
  <div id="videoId"></div>
</template>

<script setup lang="ts">
import type { FileItem } from '@/apis/system'
import Player from 'xgplayer'

interface Props {
  data: FileItem
}
const props = withDefaults(defineProps<Props>(), {})

onMounted(() => {
  new Player({
    id: 'videoId',
    url: props.data?.url ?? '',
    lang: 'zh-cn',
    autoplay: true,
    closeVideoClick: true,
    videoInit: true,
  })
})
</script>

<style scoped lang="scss"></style>
