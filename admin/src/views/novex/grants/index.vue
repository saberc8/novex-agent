<script setup lang="ts">
import type { Grant, Resource, Role } from '@/apis/novex'
import { computed, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { novex } from '@/apis/novex'
import NovexPage from '../shared/NovexPage.vue'
import { confirmDelete, scopeNames, usePageLoad, usePageState } from '../shared/page'

defineOptions({ name: 'NovexGrants' })
const { error, success, busy, loading, load, action } = usePageState()
const router = useRouter()
const grants = ref<Grant[]>([])
const resources = ref<Resource[]>([])
const roles = ref<Role[]>([])
const showGrant = ref(false)
const grantForm = reactive({
  roleId: 0,
  resourceId: '',
  scope: 'self',
  fields: {} as Record<string, string>,
})
const grantResource = computed(() =>
  resources.value.find((r) => r.id === grantForm.resourceId),
)
const roleName = (id: number) =>
  roles.value.find((r) => Number(r.id) === Number(id))?.name || String(id)
const resourceName = (id: string) =>
  resources.value.find((r) => r.id === id)?.name || id
async function refresh() {
  await load(async () => {
    const [grantData, resourceData, roleData] = await Promise.all([novex<Grant[]>('/admin/grants'), novex<Resource[]>('/admin/resources'), novex<Role[]>('/admin/roles')])
    grants.value = grantData || []
    resources.value = resourceData || []
    roles.value = roleData || []
  })
}
function openGrant() {
  Object.assign(grantForm, {
    roleId: Number(roles.value[0]?.id || 0),
    resourceId: resources.value[0]?.id || '',
    scope: 'self',
    fields: {},
  })
  showGrant.value = true
}
async function saveGrant() {
  await action(async () => {
    const fields = Object.fromEntries(
      Object.entries(grantForm.fields).filter(([, mode]) => mode !== 'hidden'),
    )
    await novex('/admin/grants', 'POST', {
      ...grantForm,
      roleId: Number(grantForm.roleId),
      fields,
    })
    showGrant.value = false
    await refresh()
  }, '授权已生效，相关旧会话将失效')
}
async function deleteGrant(item: Grant) {
  if (
    !(await confirmDelete(
      `撤销「${roleName(item.roleId)}」对「${resourceName(item.resourceId)}」的这条授权？`,
    ))
  ) {
    return
  }
  await action(async () => {
    await novex(`/admin/grants/${item.id}`, 'DELETE')
    await refresh()
  }, '授权已撤销')
}
watch(() => grantForm.resourceId, () => {
  grantForm.fields = {}
})
usePageLoad(refresh)
</script>

<template>
  <NovexPage title="角色数据授权" :loading="loading" :error="error" :success="success" @refresh="refresh" @dismiss-error="error = ''" @dismiss-success="success = ''">
    <template #actions><a-space><a-button @click="router.push('/system/role')">系统角色管理</a-button><a-button type="primary" :disabled="!resources.length || !roles.length" @click="openGrant()"><template #icon><icon-plus /></template>添加授权</a-button></a-space></template>
    <p class="nx-page-description">沿用系统角色，一条授权同时约束资源、行范围和字段。</p>
    <a-table :data="grants" row-key="id" :pagination="{ pageSize: 10 }" :scroll="{ x: 900 }">
      <template #columns>
        <a-table-column title="系统角色" :width="160"><template #cell="{ record }">{{ roleName(record.roleId) }}</template></a-table-column>
        <a-table-column title="业务资源" :width="150"><template #cell="{ record }">{{ resourceName(record.resourceId) }}</template></a-table-column>
        <a-table-column title="可访问的行" :width="130"><template #cell="{ record }"><a-tag color="arcoblue">{{ scopeNames[record.scope] || record.scope }}</a-tag></template></a-table-column>
        <a-table-column title="字段可见性" :width="420"><template #cell="{ record }"><a-space wrap><a-tag v-for="(mode, field) in record.fields" :key="field" :color="mode === 'masked' ? 'orange' : undefined">{{ field }} · {{ mode === 'masked' ? '脱敏' : '原文' }}</a-tag></a-space></template></a-table-column>
        <a-table-column title="操作" :width="90" fixed="right"><template #cell="{ record }"><a-button type="text" status="danger" size="small" :disabled="busy" @click="deleteGrant(record)">撤销</a-button></template></a-table-column>
      </template>
    </a-table>
    <a-alert class="nx-alert nx-subsection">角色分配在系统用户管理中完成。权限变更会取消相关运行，用户需要开启新对话。</a-alert>

    <a-modal v-model:visible="showGrant" title="添加角色数据授权" :footer="false" :mask-closable="false" :width="720" modal-class="novex-modal">
      <a-form :model="grantForm" layout="vertical" @submit-success="saveGrant">
        <div class="nx-form-grid"><a-form-item field="roleId" label="系统角色" required><a-select v-model="grantForm.roleId" allow-search><a-option v-for="role in roles" :key="role.id" :value="Number(role.id)">{{ role.name }} · {{ role.code }}</a-option></a-select></a-form-item><a-form-item field="resourceId" label="业务资源" required><a-select v-model="grantForm.resourceId"><a-option v-for="resource in resources" :key="resource.id" :value="resource.id">{{ resource.name }}</a-option></a-select></a-form-item></div>
        <a-form-item field="scope" label="可以访问哪些行"><a-select v-model="grantForm.scope"><a-option value="self">仅本人 · 绑定的员工编号</a-option><a-option value="department">本部门 · 绑定的部门编号</a-option><a-option value="warehouse">指定仓库 · 绑定的仓库编号</a-option><a-option value="tenant">本企业 · 仍受租户边界约束</a-option></a-select></a-form-item>
        <a-form-item field="fields" label="这部分行的字段可见性"><a-table :data="(grantResource?.columns || []).map((field) => ({ field }))" row-key="field" :pagination="false" size="small"><template #columns><a-table-column title="字段" data-index="field" /><a-table-column title="可见方式" :width="280"><template #cell="{ record }"><a-radio-group :model-value="grantForm.fields[record.field] || 'hidden'" type="button" size="small" @change="(value) => { grantForm.fields[record.field] = String(value) }"><a-radio value="hidden">隐藏</a-radio><a-radio value="plain">原文</a-radio><a-radio value="masked">脱敏</a-radio></a-radio-group></template></a-table-column></template></a-table></a-form-item>
        <a-alert class="nx-alert">未选择的字段保持隐藏。不同范围的字段权限不会跨行合并。</a-alert><a-alert v-if="error" type="error" class="nx-alert">{{ error }}</a-alert>
        <div class="nx-form-footer"><a-button @click="showGrant = false">取消</a-button><a-button html-type="submit" type="primary" :loading="busy" :disabled="!Object.values(grantForm.fields).some((value) => value !== 'hidden')">保存授权</a-button></div>
      </a-form>
    </a-modal>
  </NovexPage>
</template>

<style scoped src="../novex.css"></style>
