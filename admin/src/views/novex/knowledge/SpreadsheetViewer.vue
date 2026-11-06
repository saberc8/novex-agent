<script setup lang="ts">
import type { Knowledge, SpreadsheetSearch } from '@/apis/novex'
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { downloadKnowledge, novex } from '@/apis/novex'
import { display } from '../shared/page'

const props = defineProps<{ document: Knowledge }>()
const form = reactive({ sheet: props.document.sheets?.[0]?.name || '', query: '', column: '', op: 'eq', value: '' })
const activeSheet = computed(() => props.document.sheets?.find((sheet) => sheet.name === form.sheet))
const result = ref<SpreadsheetSearch | null>(null)
const columns = computed(() => result.value?.columns || activeSheet.value?.columns || [])
const warnings = computed(() => [...new Set([...(props.document.warnings || []), ...(result.value?.warnings || [])])])
const loading = ref(false)
const downloading = ref(false)
const error = ref('')
const offset = ref(0)
const pageSize = 20
let generation = 0
let applied: Record<string, unknown> = {}
const downloadController = new AbortController()

async function fetchRows(nextOffset: number) {
  const request = ++generation
  loading.value = true
  error.value = ''
  result.value = null
  try {
    const data = await novex<SpreadsheetSearch>(`/knowledge/${props.document.id}/search`, 'POST', { ...applied, limit: pageSize, offset: nextOffset })
    if (request !== generation) return
    result.value = data
    offset.value = nextOffset
  } catch (e) {
    if (request === generation) error.value = e instanceof Error ? e.message : '表格检索失败'
  } finally {
    if (request === generation) loading.value = false
  }
}
async function search() {
  if (!form.sheet) return
  let value: string | number = form.value
  if (form.column && ['gte', 'lte'].includes(form.op)) {
    if (!form.value.trim() || !Number.isFinite(Number(form.value))) {
      error.value = '大于等于或小于等于筛选需要输入数字。'
      return
    }
    value = Number(form.value)
  }
  applied = { sheet: form.sheet, query: form.query.trim(), filters: form.column ? [{ column: form.column, op: form.op, value }] : [] }
  await fetchRows(0)
}
function changeSheet() {
  form.column = ''
  form.value = ''
  void search()
}
async function download() {
  downloading.value = true
  error.value = ''
  try {
    await downloadKnowledge(props.document.id, props.document.fileName, downloadController.signal)
  } catch (e) {
    if (!downloadController.signal.aborted) error.value = e instanceof Error ? e.message : '下载失败'
  } finally {
    downloading.value = false
  }
}
onMounted(search)
onBeforeUnmount(() => {
  generation++
  downloadController.abort()
})
</script>

<template>
  <div class="nx-sheet-viewer">
    <div class="nx-sheet-meta"><span>{{ document.sheetCount ?? document.sheets?.length ?? 0 }} 张工作表 · {{ document.rowCount?.toLocaleString() ?? '—' }} 数据行 · 保留原始文件</span><a-button :loading="downloading" @click="download"><template #icon><icon-download /></template>下载原文件</a-button></div>
    <a-alert v-for="warning in warnings" :key="warning" type="warning" class="nx-sheet-warning">{{ warning }}</a-alert>
    <a-form :model="form" layout="vertical" @submit-success="search">
      <div class="nx-sheet-query">
        <a-form-item field="sheet" label="工作表"><a-select v-model="form.sheet" :disabled="loading" @change="changeSheet"><a-option v-for="sheet in document.sheets || []" :key="sheet.name" :value="sheet.name">{{ sheet.name }} · {{ sheet.rowCount.toLocaleString() }} 行</a-option></a-select></a-form-item>
        <a-form-item field="query" label="关键词"><a-input v-model="form.query" placeholder="搜索表格中的内容" allow-clear /></a-form-item>
      </div>
      <div class="nx-sheet-filter">
        <a-form-item field="column" label="指定列（可选）"><a-select v-model="form.column" placeholder="不限列"><a-option value="">不添加列筛选</a-option><a-option v-for="column in activeSheet?.columns || []" :key="column.key" :value="column.key">{{ column.key }} · {{ column.label }}</a-option></a-select></a-form-item>
        <a-form-item field="op" label="条件"><a-select v-model="form.op" :disabled="!form.column"><a-option value="eq">等于</a-option><a-option value="contains">包含</a-option><a-option value="gte">大于等于</a-option><a-option value="lte">小于等于</a-option></a-select></a-form-item>
        <a-form-item field="value" label="筛选值"><a-input v-model="form.value" :disabled="!form.column" placeholder="输入筛选值" /></a-form-item>
        <a-button html-type="submit" type="primary" :loading="loading" :disabled="!form.sheet">检索表格</a-button>
      </div>
    </a-form>
    <a-alert v-if="error" type="error" class="nx-sheet-warning">{{ error }}</a-alert>
    <a-table :data="result?.rows || []" row-key="row" :loading="loading" :pagination="false" :scroll="{ x: Math.max(650, columns.length * 160 + 420), y: 340 }" size="small">
      <template #columns>
        <a-table-column title="Excel 行号" :width="100" fixed="left"><template #cell="{ record }">{{ record.row }}</template></a-table-column>
        <a-table-column v-for="column in columns" :key="column.key" :title="`${column.key} · ${column.label}`" :width="160" ellipsis tooltip><template #cell="{ record }">{{ display(record.cells[column.key]) }}</template></a-table-column>
        <a-table-column title="引用位置" :width="320"><template #cell="{ record }"><span class="nx-sheet-citation">{{ record.citation }}</span></template></a-table-column>
      </template>
      <template #empty>没有匹配的数据行，可调整关键词或筛选条件。</template>
    </a-table>
    <div class="nx-sheet-pagination"><span>{{ result?.rows.length ? `匹配结果 ${offset + 1}–${offset + result.rows.length}` : '0 条结果' }} · 每页 {{ pageSize }} 行</span><a-space><a-button :disabled="loading || offset === 0" @click="fetchRows(Math.max(0, offset - pageSize))">上一页</a-button><a-button :disabled="loading || !result?.hasMore" @click="fetchRows(offset + pageSize)">下一页</a-button></a-space></div>
  </div>
</template>

<style scoped>
.nx-sheet-viewer { min-width: 0; }
.nx-sheet-meta,.nx-sheet-pagination { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 16px; color: var(--color-text-3); font-size: 12px; }
.nx-sheet-pagination { margin: 16px 0 0; }
.nx-sheet-warning { margin-bottom: 12px; }
.nx-sheet-query { display: grid; grid-template-columns: 1fr 2fr; gap: 16px; }
.nx-sheet-filter { display: grid; grid-template-columns: 1.5fr 1fr 1.5fr auto; gap: 12px; align-items: start; }
.nx-sheet-filter > .arco-btn { margin-top: 30px; }
.nx-sheet-citation { overflow-wrap: anywhere; font-family: monospace; font-size: 11px; }
@media (max-width: 700px) {
  .nx-sheet-query,.nx-sheet-filter { grid-template-columns: 1fr; gap: 0; }
  .nx-sheet-filter > .arco-btn { margin: 0 0 16px; }
  .nx-sheet-meta,.nx-sheet-pagination { align-items: flex-start; flex-direction: column; }
}
</style>
