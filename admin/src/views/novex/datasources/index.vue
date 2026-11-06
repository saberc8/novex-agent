<script setup lang="ts">
import type { Datasource } from '@/apis/novex'
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { novex } from '@/apis/novex'
import NovexPage from '../shared/NovexPage.vue'
import { usePageLoad, usePageState } from '../shared/page'

defineOptions({ name: 'NovexDatasources' })
const { error, success, busy, loading, load, action } = usePageState()
const router = useRouter()
const sources = ref<Datasource[]>([])
const testResult = ref('')
const showSource = ref(false)
const sourceForm = reactive({
  id: '',
  name: '',
  host: '',
  port: 5432,
  database: '',
  username: '',
  password: '',
  sslMode: 'disable',
  tenantId: '',
})
async function refresh() {
  await load(async () => {
    sources.value = (await novex<Datasource[]>('/admin/datasources')) || []
  })
}
function openSource(item?: Datasource) {
  Object.assign(
    sourceForm,
    {
      id: '',
      name: '',
      host: '',
      port: 5432,
      database: '',
      username: '',
      password: '',
      sslMode: 'disable',
      tenantId: '',
    },
    item || {},
  )
  sourceForm.password = ''
  testResult.value = ''
  showSource.value = true
}
async function testConnection() {
  await action(async () => {
    const result = await novex<{
      readOnly: boolean
      serverVersion: string
    }>('/admin/datasources/test', 'POST', sourceForm)
    testResult.value = `连接成功 · ${result.readOnly ? '只读查询已启用' : '请检查只读配置'} · PostgreSQL ${result.serverVersion}`
  })
}
async function saveSource() {
  await action(async () => {
    await novex(
      `/admin/datasources${sourceForm.id ? `/${sourceForm.id}` : ''}`,
      sourceForm.id ? 'PUT' : 'POST',
      sourceForm,
    )
    sourceForm.password = ''
    showSource.value = false
    await refresh()
  }, '数据连接已保存')
}
function configureResources(id: string) {
  void router.push({ path: '/novex/resources', query: { datasourceId: id } })
}
usePageLoad(refresh)
</script>

<template>
  <NovexPage title="数据连接" :loading="loading" :error="error" :success="success" @refresh="refresh" @dismiss-error="error = ''" @dismiss-success="success = ''">
    <template #actions><a-button type="primary" @click="openSource()"><template #icon><icon-plus /></template>新建连接</a-button></template>
    <p class="nx-page-description">接入已有 PostgreSQL，业务数据保留在原数据库。</p>
    <a-table :data="sources" row-key="id" :loading="loading" :pagination="false" :scroll="{ x: 950 }">
      <template #columns>
        <a-table-column title="连接名称" :width="220"><template #cell="{ record }"><span>{{ record.name }}</span><small class="nx-secondary nx-block">{{ record.id }}</small></template></a-table-column>
        <a-table-column title="数据库" data-index="database" :width="160" />
        <a-table-column title="服务器" :width="200"><template #cell="{ record }">{{ record.host }}:{{ record.port }}</template></a-table-column>
        <a-table-column title="连接账号" data-index="username" :width="200" ellipsis tooltip />
        <a-table-column title="SSL 模式" :width="120"><template #cell="{ record }"><a-tag>{{ record.sslMode }}</a-tag></template></a-table-column>
        <a-table-column title="操作" :width="210" fixed="right"><template #cell="{ record }"><a-space><a-button type="text" size="small" @click="openSource(record)">编辑 / 测试</a-button><a-button type="text" size="small" :disabled="busy" @click="configureResources(record.id)">配置资源</a-button></a-space></template></a-table-column>
      </template>
    </a-table>

    <a-modal v-model:visible="showSource" :title="sourceForm.id ? '编辑数据连接' : '新建数据连接'" :footer="false" :mask-closable="false" :width="600" modal-class="novex-modal">
      <a-form :model="sourceForm" layout="vertical" @submit-success="saveSource">
        <a-form-item field="name" label="连接名称" required><a-input v-model="sourceForm.name" placeholder="如：人事业务库" /></a-form-item>
        <div class="nx-form-grid nx-host-grid"><a-form-item field="host" label="主机" required><a-input v-model="sourceForm.host" placeholder="数据库主机地址" /></a-form-item><a-form-item field="port" label="端口" required><a-input-number v-model="sourceForm.port" :min="1" :max="65535" /></a-form-item></div>
        <a-form-item field="database" label="数据库" required><a-input v-model="sourceForm.database" placeholder="数据库名称" /></a-form-item>
        <div class="nx-form-grid"><a-form-item field="username" label="用户名" required><a-input v-model="sourceForm.username" autocomplete="off" /></a-form-item><a-form-item field="password" label="密码" :required="!sourceForm.id"><a-input-password v-model="sourceForm.password" autocomplete="new-password" :placeholder="sourceForm.id ? '留空保留已保存的密码' : '数据库密码'" /></a-form-item></div>
        <a-form-item field="tenantId" label="绑定内部租户（可选）" extra="留空为共享多租户库；绑定到单一租户后可不配置租户列。"><a-input v-model="sourceForm.tenantId" placeholder="内部租户编号" /></a-form-item>
        <a-form-item field="sslMode" label="SSL 模式"><a-select v-model="sourceForm.sslMode"><a-option value="disable">关闭（本地开发）</a-option><a-option value="require">要求加密</a-option><a-option value="verify-full">校验证书与主机</a-option></a-select></a-form-item>
        <a-alert v-if="testResult" type="success" class="nx-alert">{{ testResult }}</a-alert><a-alert v-if="error" type="error" class="nx-alert">{{ error }}</a-alert>
        <div class="nx-form-footer"><a-button :disabled="busy" @click="testConnection">测试连接</a-button><a-button @click="showSource = false">取消</a-button><a-button html-type="submit" type="primary" :loading="busy">保存连接</a-button></div>
      </a-form>
    </a-modal>
  </NovexPage>
</template>

<style scoped src="../novex.css"></style>
