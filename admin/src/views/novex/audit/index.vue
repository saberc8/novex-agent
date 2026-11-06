<script setup lang="ts">
import type { Audit } from '@/apis/novex'
import { ref } from 'vue'
import { novex } from '@/apis/novex'
import NovexPage from '../shared/NovexPage.vue'
import { display, usePageLoad, usePageState, when } from '../shared/page'

defineOptions({ name: 'NovexAudit' })
const { error, success, loading, load } = usePageState()
const audits = ref<Audit[]>([])
async function refresh() {
  await load(async () => {
    audits.value = (await novex<Audit[]>('/admin/audit?limit=100')) || []
  })
}
usePageLoad(refresh)
</script>

<template>
  <NovexPage title="运行审计" :loading="loading" :error="error" :success="success" @refresh="refresh" @dismiss-error="error = ''" @dismiss-success="success = ''">
    <p class="nx-page-description">查看访问结果与执行原因，不记录连接密码或敏感数据原文。</p>
    <a-table :data="audits" row-key="id" :pagination="{ pageSize: 15 }" :scroll="{ x: 1000 }">
      <template #columns>
        <a-table-column title="时间" :width="180"><template #cell="{ record }">{{ when(record.createdAt) }}</template></a-table-column>
        <a-table-column title="账号" :width="100"><template #cell="{ record }">{{ record.userId }}</template></a-table-column>
        <a-table-column title="动作" data-index="action" :width="180" />
        <a-table-column title="资源" :width="150"><template #cell="{ record }">{{ record.resourceId || '—' }}</template></a-table-column>
        <a-table-column title="结果" :width="90"><template #cell="{ record }"><a-tag :color="record.allowed ? 'green' : 'red'">{{ record.allowed ? '允许' : '拒绝' }}</a-tag></template></a-table-column>
        <a-table-column title="详情" :width="360"><template #cell="{ record }"><span class="nx-audit-detail">{{ display(record.detail) }}</span></template></a-table-column>
      </template>
    </a-table>
  </NovexPage>
</template>

<style scoped src="../novex.css"></style>
