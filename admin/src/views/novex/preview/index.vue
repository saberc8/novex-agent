<script setup lang="ts">
import type { NovexUser, Preview, Resource } from '@/apis/novex'
import { computed, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { novex } from '@/apis/novex'
import NovexPage from '../shared/NovexPage.vue'
import { display, usePageLoad, usePageState } from '../shared/page'

defineOptions({ name: 'NovexPreview' })
const { error, success, busy, loading, load, action } = usePageState()
const route = useRoute()
const users = ref<NovexUser[]>([])
const resources = ref<Resource[]>([])
const preview = ref<Preview | null>(null)
const previewForm = reactive({
  userId: 0,
  resourceId: '',
  fields: [] as string[],
  filterField: '',
  filterOp: 'eq',
  filterValue: '',
  limit: 10,
})
const enabledUsers = computed(() => users.value.filter((user) => user.novexEnabled))
const previewResource = computed(() =>
  resources.value.find((r) => r.id === previewForm.resourceId),
)
async function refresh() {
  await load(async () => {
    const [userData, resourceData] = await Promise.all([novex<NovexUser[]>('/admin/users'), novex<Resource[]>('/admin/resources')])
    users.value = userData || []
    resources.value = resourceData || []
  })
}
async function runPreview() {
  await action(async () => {
    preview.value = null
    preview.value = await novex<Preview>('/admin/query/preview', 'POST', {
      userId: Number(previewForm.userId),
      resourceId: previewForm.resourceId,
      fields: previewForm.fields.length ? previewForm.fields : undefined,
      filters: previewForm.filterField
        ? [
            {
              field: previewForm.filterField,
              op: previewForm.filterOp,
              value: previewForm.filterValue,
            },
          ]
        : [],
      limit: Number(previewForm.limit),
    })
  })
}
watch(() => previewForm.resourceId, () => {
  previewForm.fields = []
  previewForm.filterField = ''
  preview.value = null
})
watch(() => route.query.resourceId, (value) => {
  if (route.path === '/novex/preview' && typeof value === 'string') previewForm.resourceId = value
}, { immediate: true })
usePageLoad(refresh)
</script>

<template>
  <NovexPage title="权限预览" :loading="loading" :error="error" :success="success" @refresh="refresh" @dismiss-error="error = ''" @dismiss-success="success = ''">
    <p class="nx-page-description">检查指定账号可见的行与字段；预览使用占位值，真实数据请登录对应账号查询。</p>
    <a-form :model="previewForm" layout="vertical" @submit-success="runPreview">
      <div class="nx-form-grid">
        <a-form-item field="userId" label="测试账号" required><a-select v-model="previewForm.userId" placeholder="选择账号" allow-search><a-option :value="0" disabled>选择账号</a-option><a-option v-for="user in enabledUsers" :key="user.id" :value="Number(user.id)">{{ user.username }} · {{ user.tenantId }} · {{ user.roles?.map((r) => r.name).join(' / ') }}</a-option></a-select></a-form-item>
        <a-form-item field="resourceId" label="业务资源" required><a-select v-model="previewForm.resourceId" placeholder="选择资源"><a-option v-for="resource in resources" :key="resource.id" :value="resource.id">{{ resource.name }}</a-option></a-select></a-form-item>
      </div>
      <a-form-item v-if="previewResource" field="fields" label="请求字段" extra="不选则由服务端返回已授权字段"><a-checkbox-group v-model="previewForm.fields" :options="previewResource.columns" /></a-form-item>
      <div class="nx-form-grid nx-filter-grid">
        <a-form-item field="filterField" label="过滤字段（可选）"><a-select v-model="previewForm.filterField"><a-option value="">不过滤</a-option><a-option v-for="field in previewResource?.columns || []" :key="field" :value="field">{{ field }}</a-option></a-select></a-form-item>
        <a-form-item field="filterOp" label="条件"><a-select v-model="previewForm.filterOp"><a-option value="eq">等于</a-option><a-option value="contains">包含</a-option><a-option value="gte">大于等于</a-option><a-option value="lte">小于等于</a-option></a-select></a-form-item>
        <a-form-item field="filterValue" label="值"><a-input v-model="previewForm.filterValue" :disabled="!previewForm.filterField" placeholder="输入过滤值" /></a-form-item>
        <a-form-item field="limit" label="最多返回"><a-input-number v-model="previewForm.limit" :min="1" :max="100" /></a-form-item>
      </div>
      <a-button html-type="submit" type="primary" :loading="busy" :disabled="!previewForm.userId || !previewForm.resourceId">执行权限预览</a-button>
    </a-form>
    <a-card v-if="preview" :title="`${preview.resource.name} · ${preview.rows.length} 行`" class="nx-subsection" size="small">
      <template #extra><a-tag>权限版本 {{ preview.policyVersion }}</a-tag></template>
      <a-table :data="preview.rows" :pagination="false" :scroll="{ x: '100%' }"><template #columns><a-table-column v-for="field in preview.columns" :key="field" :title="field" :width="140"><template #cell="{ record }">{{ display(record[field]) }}</template></a-table-column></template></a-table>
      <a-collapse class="nx-subsection"><a-collapse-item key="explanation" header="权限解释"><pre class="nx-pre">{{ typeof preview.explanation === 'string' ? preview.explanation : JSON.stringify(preview.explanation, null, 2) }}</pre></a-collapse-item></a-collapse>
    </a-card>
  </NovexPage>
</template>

<style scoped src="../novex.css"></style>
