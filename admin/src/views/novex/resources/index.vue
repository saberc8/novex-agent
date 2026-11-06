<script setup lang="ts">
import type { Datasource, Resource, SchemaTable } from '@/apis/novex'
import { computed, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { novex } from '@/apis/novex'
import NovexPage from '../shared/NovexPage.vue'
import { usePageLoad, usePageState } from '../shared/page'

defineOptions({ name: 'NovexResources' })
const { error, success, busy, loading, load, action } = usePageState()
const router = useRouter()
const route = useRoute()
const sources = ref<Datasource[]>([])
const resources = ref<Resource[]>([])
const tables = ref<SchemaTable[]>([])
const scannedSourceId = ref('')
const selectedSourceId = ref('')
const resourceColumnSnapshot = ref<string[]>([])
const showResource = ref(false)
const resourceForm = reactive({
  id: '',
  name: '',
  key: '',
  datasourceId: '',
  schema: '',
  table: '',
  tenantColumn: '',
  ownerColumn: '',
  departmentColumn: '',
  warehouseColumn: '',
  identityRequired: true,
  columns: [] as string[],
})
const resourceTables = computed(() =>
  scannedSourceId.value === resourceForm.datasourceId ? tables.value : [],
)
const selectedTable = computed(() =>
  resourceTables.value.find(
    (t) => t.schema === resourceForm.schema && t.name === resourceForm.table,
  ),
)
const resourceColumnOptions = computed(() =>
  selectedTable.value?.columns.map((column) => column.name) || resourceColumnSnapshot.value,
)
const sourceName = (id: string) =>
  sources.value.find((s) => s.id === id)?.name || id
async function refresh() {
  await load(async () => {
    const [resourceData, sourceData] = await Promise.all([novex<Resource[]>('/admin/resources'), novex<Datasource[]>('/admin/datasources')])
    resources.value = resourceData || []
    sources.value = sourceData || []
    if (!selectedSourceId.value) selectedSourceId.value = sources.value[0]?.id || ''
  })
}
function openResource(item?: Resource) {
  Object.assign(
    resourceForm,
    {
      id: '',
      name: '',
      key: '',
      datasourceId: selectedSourceId.value || scannedSourceId.value || sources.value[0]?.id || '',
      schema: '',
      table: '',
      tenantColumn: '',
      ownerColumn: '',
      departmentColumn: '',
      warehouseColumn: '',
      identityRequired: true,
      columns: [],
    },
    item || {},
  )
  resourceForm.columns = [...(item?.columns || [])]
  resourceColumnSnapshot.value = [...new Set([
    ...(item?.columns || []),
    item?.tenantColumn,
    item?.ownerColumn,
    item?.departmentColumn,
    item?.warehouseColumn,
  ].filter((column): column is string => !!column))]
  showResource.value = true
}
function resourceSourceChanged() {
  resourceForm.schema = ''
  resourceForm.table = ''
  resourceForm.columns = []
  resourceForm.tenantColumn = ''
  resourceForm.ownerColumn = ''
  resourceForm.departmentColumn = ''
  resourceForm.warehouseColumn = ''
  resourceColumnSnapshot.value = []
}
function chooseTable(value: string | number | Record<string, unknown> | undefined) {
  const item = resourceTables.value.find((t) => `${t.schema}.${t.name}` === value)
  if (item) {
    resourceColumnSnapshot.value = item.columns.map((column) => column.name)
    resourceForm.schema = item.schema
    resourceForm.table = item.name
    resourceForm.columns = []
    resourceForm.tenantColumn = ''
    resourceForm.ownerColumn = ''
    resourceForm.departmentColumn = ''
    resourceForm.warehouseColumn = ''
  }
}
async function scanForForm() {
  const datasourceId = resourceForm.datasourceId
  await action(async () => {
    const result = await novex<{
      tables: SchemaTable[]
    }>(
      `/admin/datasources/${datasourceId}/introspect`,
      'POST',
      {},
    )
    tables.value = result.tables || []
    scannedSourceId.value = datasourceId
  })
}
async function saveResource() {
  await action(async () => {
    await novex(
      `/admin/resources${resourceForm.id ? `/${resourceForm.id}` : ''}`,
      resourceForm.id ? 'PUT' : 'POST',
      resourceForm,
    )
    showResource.value = false
    await refresh()
  }, '业务资源已保存')
}
async function scanSource() {
  const sourceId = selectedSourceId.value
  if (!sourceId) return
  await action(async () => {
    const result = await novex<{ tables: SchemaTable[] }>(`/admin/datasources/${sourceId}/introspect`, 'POST', {})
    tables.value = result.tables || []
    scannedSourceId.value = sourceId
  }, '数据库结构已扫描，可以创建业务资源')
}
watch(() => route.query.datasourceId, (value) => {
  if (route.path === '/novex/resources' && typeof value === 'string') selectedSourceId.value = value
}, { immediate: true })
usePageLoad(refresh)
</script>

<template>
  <NovexPage title="业务资源" :loading="loading" :error="error" :success="success" @refresh="refresh" @dismiss-error="error = ''" @dismiss-success="success = ''">
    <template #actions><a-button type="primary" :disabled="!sources.length" @click="openResource()"><template #icon><icon-plus /></template>新建资源</a-button></template>
    <p class="nx-page-description">将数据表映射成可授权资源，仅注册的字段可以被查询。</p>
    <a-space class="nx-source-scan" wrap>
      <a-select v-model="selectedSourceId" placeholder="选择要扫描的数据连接" style="width: 280px" allow-search>
        <a-option v-for="source in sources" :key="source.id" :value="source.id">{{ source.name }}</a-option>
      </a-select>
      <a-button :loading="busy" :disabled="!selectedSourceId" @click="scanSource">扫描数据库结构</a-button>
      <span class="nx-secondary">按需扫描表与字段，再创建业务资源。</span>
    </a-space>
    <a-alert v-if="scannedSourceId && tables.length" type="success" class="nx-alert">已扫描 {{ sourceName(scannedSourceId) }} · {{ tables.length }} 张表<template #title>数据库结构已加载</template></a-alert>
    <div class="nx-resource-grid">
      <a-card v-for="resource in resources" :key="resource.id" :title="resource.name" size="small">
        <template #extra><a-button type="text" size="small" @click="openResource(resource)">编辑</a-button></template>
        <p class="nx-resource-location">{{ sourceName(resource.datasourceId) }} / {{ resource.schema }}.{{ resource.table }}</p>
        <a-descriptions :column="2" size="small">
          <a-descriptions-item label="编码">{{ resource.key }}</a-descriptions-item>
          <a-descriptions-item label="租户">{{ resource.tenantColumn || '独立库绑定' }}</a-descriptions-item>
          <a-descriptions-item label="本人">{{ resource.ownerColumn || '未映射' }}</a-descriptions-item>
          <a-descriptions-item label="部门">{{ resource.departmentColumn || '未映射' }}</a-descriptions-item>
          <a-descriptions-item label="仓库">{{ resource.warehouseColumn || '未映射' }}</a-descriptions-item>
        </a-descriptions>
        <a-space wrap class="nx-resource-fields"><a-tag v-for="column in resource.columns" :key="column">{{ column }}</a-tag></a-space>
        <div class="nx-resource-footer"><span class="nx-secondary">{{ resource.identityRequired ? '需要外部身份绑定' : '无需外部身份绑定' }}</span><a-button type="text" size="small" @click="router.push({ path: '/novex/preview', query: { resourceId: resource.id } })">预览权限</a-button></div>
      </a-card>
    </div>
    <a-empty v-if="!resources.length" description="尚未配置业务资源，请先扫描数据连接。" />

    <a-modal v-model:visible="showResource" :title="resourceForm.id ? '编辑业务资源' : '新建业务资源'" :footer="false" :mask-closable="false" :width="720" modal-class="novex-modal">
      <a-form :model="resourceForm" layout="vertical" @submit-success="saveResource">
        <div class="nx-form-grid"><a-form-item field="name" label="显示名称" required><a-input v-model="resourceForm.name" placeholder="如：工资明细" /></a-form-item><a-form-item field="key" label="资源编码" :rules="[{ required: true, match: /^[a-zA-Z0-9_-]+$/, message: '请使用字母、数字、下划线或连字符' }]"><a-input v-model="resourceForm.key" placeholder="如：payroll" /></a-form-item></div>
        <a-form-item field="datasourceId" label="数据连接" required><a-space fill class="nx-source-select"><a-select v-model="resourceForm.datasourceId" @change="resourceSourceChanged"><a-option v-for="source in sources" :key="source.id" :value="source.id">{{ source.name }}</a-option></a-select><a-button :disabled="busy || !resourceForm.datasourceId" @click="scanForForm">扫描数据表</a-button></a-space></a-form-item>
        <a-form-item field="table" label="数据表" required><a-select :model-value="resourceForm.table ? `${resourceForm.schema}.${resourceForm.table}` : undefined" placeholder="先扫描，再选择表" @change="chooseTable"><a-option v-for="table in resourceTables" :key="`${table.schema}.${table.name}`" :value="`${table.schema}.${table.name}`">{{ table.schema }}.{{ table.name }}</a-option><a-option v-if="resourceForm.table && !selectedTable" :value="`${resourceForm.schema}.${resourceForm.table}`">{{ resourceForm.schema }}.{{ resourceForm.table }}（已保存，扫描可编辑）</a-option></a-select></a-form-item>
        <a-divider orientation="left">身份字段映射</a-divider>
        <div class="nx-form-grid">
          <a-form-item
            v-for="mapping in [
              { key: 'tenantColumn', label: '外部租户字段' }, { key: 'ownerColumn', label: '本人字段' },
              { key: 'departmentColumn', label: '部门字段' }, { key: 'warehouseColumn', label: '仓库字段' },
            ]" :key="mapping.key" :field="mapping.key" :label="mapping.label" :required="mapping.key === 'tenantColumn' && !sources.find((s) => s.id === resourceForm.datasourceId)?.tenantId"
          >
            <a-select v-model="resourceForm[mapping.key as 'tenantColumn' | 'ownerColumn' | 'departmentColumn' | 'warehouseColumn']"><a-option value="">不映射</a-option><a-option v-for="column in resourceColumnOptions" :key="column" :value="column">{{ column }}</a-option></a-select>
          </a-form-item>
        </div>
        <a-form-item field="columns" label="开放的字段" required><a-checkbox-group v-model="resourceForm.columns" :options="resourceColumnOptions" /><template #extra>只有注册并授权的字段可以被查询。</template></a-form-item>
        <a-form-item field="identityRequired"><a-checkbox v-model="resourceForm.identityRequired">查询此资源需要外部身份绑定</a-checkbox></a-form-item>
        <a-alert v-if="error" type="error" class="nx-alert">{{ error }}</a-alert>
        <div class="nx-form-footer"><a-button @click="showResource = false">取消</a-button><a-button html-type="submit" type="primary" :loading="busy" :disabled="!resourceForm.columns.length || (!resourceForm.tenantColumn && !sources.find((s) => s.id === resourceForm.datasourceId)?.tenantId)">保存资源</a-button></div>
      </a-form>
    </a-modal>
  </NovexPage>
</template>

<style scoped src="../novex.css"></style>
