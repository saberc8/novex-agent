<script setup lang="ts">
import type { Knowledge, NovexUser } from '@/apis/novex'
import { computed, reactive, ref } from 'vue'
import { novex } from '@/apis/novex'
import NovexPage from '../shared/NovexPage.vue'
import { confirmDelete, scopeNames, usePageLoad, usePageState, when } from '../shared/page'
import SpreadsheetViewer from './SpreadsheetViewer.vue'

defineOptions({ name: 'NovexKnowledge' })
const { error, success, busy, loading, load, action } = usePageState()
const documents = ref<Knowledge[]>([])
const users = ref<NovexUser[]>([])
const showUpload = ref(false)
const docText = ref<Knowledge | null>(null)
const editAcl = ref<Knowledge | null>(null)
const uploadForm = reactive({
  title: '',
  scope: 'self',
  file: null as File | null,
})
const aclForm = reactive({ scope: 'self', ownerId: '', departmentId: '' })
const enabledUsers = computed(() => users.value.filter((user) => user.novexEnabled))
const userName = (id: string | number) =>
  users.value.find((u) => Number(u.id) === Number(id))?.username || String(id)
async function refresh() {
  await load(async () => {
    documents.value = (await novex<Knowledge[]>('/knowledge')) || []
  })
}
async function uploadFile() {
  if (!uploadForm.file) return
  await action(async () => {
    const data = new FormData()
    data.append('file', uploadForm.file!)
    data.append('title', uploadForm.title)
    data.append('scope', uploadForm.scope)
    await novex('/knowledge', 'POST', data)
    showUpload.value = false
    uploadForm.file = null
    uploadForm.title = ''
    await refresh()
  }, '资料已导入')
}
async function saveAcl() {
  if (!editAcl.value) return
  await action(async () => {
    await novex(`/knowledge/${editAcl.value!.id}/acl`, 'PUT', aclForm)
    editAcl.value = null
    await refresh()
  }, '资料访问权限已更新')
}
async function viewDocument(item: Knowledge) {
  await action(async () => {
    docText.value = await novex<Knowledge>(`/knowledge/${item.id}`)
  })
}
async function deleteDocument(item: Knowledge) {
  if (
    !(await confirmDelete(
      `删除资料「${item.title}」？相关运行和旧会话将失效。`,
    ))
  ) {
    return
  }
  await action(async () => {
    await novex(`/knowledge/${item.id}`, 'DELETE')
    await refresh()
  }, '资料已删除')
}
async function openAcl(item: Knowledge) {
  await action(async () => {
    users.value = (await novex<NovexUser[]>('/admin/users')) || []
    editAcl.value = item
    Object.assign(aclForm, { scope: item.scope, ownerId: item.ownerId, departmentId: item.departmentId || '' })
  })
}
usePageLoad(refresh)
</script>

<template>
  <NovexPage title="知识资料" :loading="loading" :error="error" :success="success" @refresh="refresh" @dismiss-error="error = ''" @dismiss-success="success = ''">
    <template #actions><a-button type="primary" @click="showUpload = true"><template #icon><icon-upload /></template>上传资料</a-button></template>
    <p class="nx-page-description">文档转换为 Markdown；Excel / CSV 保留原文件，按工作表与数据行直接检索。</p>
    <a-table :data="documents" row-key="id" :pagination="{ pageSize: 10 }" :scroll="{ x: 1000 }">
      <template #columns>
        <a-table-column title="资料" :width="260"><template #cell="{ record }">{{ record.title }}<small class="nx-secondary nx-block">{{ record.fileName }}</small></template></a-table-column>
        <a-table-column title="所属企业" :width="110"><template #cell="{ record }">{{ record.tenantId || '—' }}</template></a-table-column>
        <a-table-column title="访问范围" :width="110"><template #cell="{ record }"><a-tag>{{ scopeNames[record.scope] }}</a-tag></template></a-table-column>
        <a-table-column title="拥有者" :width="100"><template #cell="{ record }">{{ userName(record.ownerId) }}</template></a-table-column>
        <a-table-column title="格式与规模" :width="180"><template #cell="{ record }"><template v-if="record.kind === 'spreadsheet'"><a-tag color="green">{{ record.fileName?.toLowerCase().endsWith('.csv') ? 'CSV' : 'Excel' }}</a-tag><small class="nx-secondary nx-block">{{ record.sheetCount ?? 0 }} 张工作表 · {{ record.rowCount?.toLocaleString() ?? 0 }} 行</small></template><template v-else><a-tag>Markdown</a-tag><small class="nx-secondary nx-block">{{ record.characters?.toLocaleString() ?? 0 }} 字符</small></template></template></a-table-column>
        <a-table-column title="上传时间" :width="180"><template #cell="{ record }">{{ when(record.createdAt) }}</template></a-table-column>
        <a-table-column title="操作" :width="210" fixed="right"><template #cell="{ record }"><a-space><a-button type="text" size="small" @click="viewDocument(record)">查看</a-button><a-button type="text" size="small" @click="openAcl(record)">权限</a-button><a-button type="text" status="danger" size="small" @click="deleteDocument(record)">删除</a-button></a-space></template></a-table-column>
      </template>
    </a-table>

    <a-modal v-model:visible="showUpload" title="上传知识资料" :footer="false" :mask-closable="false" :width="560" modal-class="novex-modal">
      <a-form :model="uploadForm" layout="vertical" @submit-success="uploadFile">
        <a-form-item label="资料文件" required extra="支持 MD、TXT、DOCX、文字型 PDF、XLSX、CSV。表格文件最多 32 MiB、20 万数据行；不支持 XLS / XLSM。Excel 权限作用于整个文件和全部工作表；需要按员工、部门或字段隔离时，请接入业务数据库配置数据授权。"><input class="nx-file-input" type="file" accept=".md,.txt,.docx,.pdf,.xlsx,.csv" @change="uploadForm.file = ($event.target as HTMLInputElement).files?.[0] || null" /></a-form-item>
        <a-form-item field="title" label="资料名称"><a-input v-model="uploadForm.title" placeholder="留空则使用文件名" /></a-form-item>
        <a-form-item field="scope" label="访问范围"><a-select v-model="uploadForm.scope"><a-option value="self">仅自己</a-option><a-option value="department">本部门</a-option><a-option value="tenant">本企业</a-option></a-select></a-form-item>
        <a-alert v-if="error" type="error" class="nx-alert">{{ error }}</a-alert>
        <div class="nx-form-footer"><a-button @click="showUpload = false">取消</a-button><a-button html-type="submit" type="primary" :loading="busy" :disabled="!uploadForm.file">{{ busy ? '正在导入…' : '上传资料' }}</a-button></div>
      </a-form>
    </a-modal>
    <a-modal :visible="!!editAcl" title="资料访问权限" :footer="false" :mask-closable="false" :width="560" modal-class="novex-modal" @cancel="editAcl = null">
      <a-alert class="nx-alert">{{ editAcl?.title }} · 租户归属不可修改</a-alert>
      <a-form :model="aclForm" layout="vertical" @submit-success="saveAcl">
        <a-form-item field="scope" label="谁可以访问"><a-select v-model="aclForm.scope"><a-option value="self">仅拥有者</a-option><a-option value="department">所属部门</a-option><a-option value="tenant">本企业</a-option></a-select></a-form-item>
        <a-form-item field="ownerId" label="拥有者" required><a-select v-model="aclForm.ownerId" allow-search><a-option v-for="user in enabledUsers" :key="user.id" :value="String(user.id)">{{ user.username }} · {{ user.tenantId }}</a-option></a-select></a-form-item>
        <a-form-item v-if="aclForm.scope === 'department'" field="departmentId" label="部门编号" required><a-input v-model="aclForm.departmentId" /></a-form-item>
        <a-alert v-if="error" type="error" class="nx-alert">{{ error }}</a-alert>
        <div class="nx-form-footer"><a-button @click="editAcl = null">取消</a-button><a-button html-type="submit" type="primary" :loading="busy">保存权限</a-button></div>
      </a-form>
    </a-modal>
    <a-modal :visible="!!docText" :title="docText?.title" :footer="false" :width="docText?.kind === 'spreadsheet' ? 1100 : 800" modal-class="novex-modal" @cancel="docText = null">
      <SpreadsheetViewer v-if="docText?.kind === 'spreadsheet'" :key="docText.id" :document="docText" />
      <pre v-else class="nx-pre nx-document-text">{{ docText?.markdown }}</pre>
      <div class="nx-form-footer"><span class="nx-secondary">{{ docText?.fileName }}</span><a-button @click="docText = null">关闭</a-button></div>
    </a-modal>
  </NovexPage>
</template>

<style scoped src="../novex.css"></style>
