<script setup lang="ts">
import type { Datasource, Identity, NovexUser } from '@/apis/novex'
import { computed, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { novex } from '@/apis/novex'
import has from '@/utils/has'
import NovexPage from '../shared/NovexPage.vue'
import { usePageLoad, usePageState } from '../shared/page'

defineOptions({ name: 'NovexIdentities' })
const { error, success, busy, loading, load, action } = usePageState()
const router = useRouter()
const sources = ref<Datasource[]>([])
const users = ref<NovexUser[]>([])
const identities = ref<Identity[]>([])
const showIdentity = ref(false)
const profileUser = ref<NovexUser | null>(null)
const profileForm = reactive({ tenantId: '', departmentId: '' })
const enabledUsers = computed(() => users.value.filter((user) => user.novexEnabled))
const canEnableUser = computed(() => has.hasPerm('novex:user:enable'))
const identityForm = reactive({
  id: '',
  userId: 0,
  datasourceId: '',
  tenantId: '',
  employeeId: '',
  externalTenantId: '',
  departmentId: '',
  warehouseText: '',
})
const sourceName = (id: string) =>
  sources.value.find((s) => s.id === id)?.name || id
const userName = (id: string | number) =>
  users.value.find((u) => Number(u.id) === Number(id))?.username || String(id)
async function refresh() {
  await load(async () => {
    const [userData, identityData, sourceData] = await Promise.all([novex<NovexUser[]>('/admin/users'), novex<Identity[]>('/admin/identities'), novex<Datasource[]>('/admin/datasources')])
    users.value = userData || []
    identities.value = identityData || []
    sources.value = sourceData || []
  })
}
function openProfile(user: NovexUser) {
  profileUser.value = user
  Object.assign(profileForm, { tenantId: user.tenantId || '', departmentId: user.departmentId || '' })
}
async function saveProfile() {
  if (!profileUser.value) return
  await action(async () => {
    await novex(`/admin/users/${profileUser.value!.id}/profile`, 'PUT', profileForm)
    profileUser.value = null
    await refresh()
  }, '业务档案已保存，请继续配置外部身份和数据授权')
}
function openIdentity(item?: Identity) {
  Object.assign(
    identityForm,
    {
      id: '',
      userId: Number(enabledUsers.value[0]?.id || 0),
      datasourceId: sources.value[0]?.id || '',
      tenantId: enabledUsers.value[0]?.tenantId || '',
      employeeId: '',
      externalTenantId: '',
      departmentId: '',
      warehouseText: '',
    },
    item || {},
  )
  identityForm.warehouseText = item?.warehouseIds?.join(', ') || ''
  showIdentity.value = true
}
function identityUserChanged() {
  const selected = users.value.find(
    (u) => Number(u.id) === Number(identityForm.userId),
  )
  identityForm.tenantId = selected?.tenantId || ''
  clearExternalIdentity()
}
function clearExternalIdentity() {
  identityForm.employeeId = ''
  identityForm.externalTenantId = ''
  identityForm.departmentId = ''
  identityForm.warehouseText = ''
}
async function saveIdentity() {
  await action(async () => {
    await novex(
      `/admin/identities${identityForm.id ? `/${identityForm.id}` : ''}`,
      identityForm.id ? 'PUT' : 'POST',
      {
        ...identityForm,
        userId: Number(identityForm.userId),
        warehouseIds: identityForm.warehouseText
          .split(',')
          .map((s) => s.trim())
          .filter(Boolean),
      },
    )
    showIdentity.value = false
    await refresh()
  }, '外部身份绑定已更新，相关旧会话将失效')
}
usePageLoad(refresh)
</script>

<template>
  <NovexPage title="用户业务身份" :loading="loading" :error="error" :success="success" @refresh="refresh" @dismiss-error="error = ''" @dismiss-success="success = ''">
    <div class="nx-section-heading"><div><h3>系统用户与业务档案</h3><p>复用系统用户和系统角色。先开通业务档案，再绑定外部数据库身份。</p></div><a-space><a-button @click="router.push('/system/user')">系统用户管理</a-button><a-button @click="router.push('/system/role')">系统角色管理</a-button></a-space></div>
    <a-table :data="users" row-key="id" :loading="loading" :pagination="{ pageSize: 10 }" :scroll="{ x: 850 }">
      <template #columns>
        <a-table-column title="系统账号" :width="170"><template #cell="{ record }">{{ record.nickname || record.username }}<small class="nx-secondary nx-block">{{ record.username }}</small></template></a-table-column>
        <a-table-column title="系统角色" :width="280"><template #cell="{ record }"><a-space wrap><a-tag v-for="role in record.roles" :key="role.id">{{ role.name }}</a-tag></a-space></template></a-table-column>
        <a-table-column title="业务档案" :width="110"><template #cell="{ record }"><a-tag :color="record.novexEnabled ? 'green' : undefined">{{ record.novexEnabled ? '已开通' : '未开通' }}</a-tag></template></a-table-column>
        <a-table-column title="内部租户" :width="140"><template #cell="{ record }">{{ record.tenantId || '—' }}</template></a-table-column>
        <a-table-column title="业务部门" :width="120"><template #cell="{ record }">{{ record.departmentId || '—' }}</template></a-table-column>
        <a-table-column title="操作" :width="160" fixed="right"><template #cell="{ record }"><a-button type="text" size="small" :disabled="!canEnableUser" @click="openProfile(record)">{{ record.novexEnabled ? '修改业务档案' : '开通业务档案' }}</a-button></template></a-table-column>
      </template>
    </a-table>
    <div class="nx-section-heading nx-subsection"><div><h3>外部身份绑定</h3><p>系统账号与每个数据源分别关联；内部租户和外部公司编号可以不同。</p></div><a-button type="primary" :disabled="!sources.length || !enabledUsers.length" @click="openIdentity()"><template #icon><icon-plus /></template>绑定身份</a-button></div>
    <a-table :data="identities" row-key="id" :pagination="{ pageSize: 10 }" :scroll="{ x: 1100 }">
      <template #columns>
        <a-table-column title="系统账号" :width="100"><template #cell="{ record }">{{ userName(record.userId) }}</template></a-table-column>
        <a-table-column title="数据连接" :width="160"><template #cell="{ record }">{{ sourceName(record.datasourceId) }}</template></a-table-column>
        <a-table-column title="内部租户" data-index="tenantId" :width="120" />
        <a-table-column title="外部租户" :width="120"><template #cell="{ record }">{{ record.externalTenantId || record.tenantId }}</template></a-table-column>
        <a-table-column title="外部员工编号" data-index="employeeId" :width="140" />
        <a-table-column title="外部部门" :width="120"><template #cell="{ record }">{{ record.departmentId || '—' }}</template></a-table-column>
        <a-table-column title="仓库" :width="130"><template #cell="{ record }">{{ record.warehouseIds?.join(', ') || '—' }}</template></a-table-column>
        <a-table-column title="操作" :width="130" fixed="right"><template #cell="{ record }"><a-button type="text" size="small" @click="openIdentity(record)">修改绑定</a-button></template></a-table-column>
      </template>
    </a-table>

    <a-modal :visible="!!profileUser" :title="`${profileUser?.novexEnabled ? '修改' : '开通'}业务档案 · ${profileUser?.username || ''}`" :footer="false" :mask-closable="false" :width="560" modal-class="novex-modal" @cancel="profileUser = null">
      <a-alert class="nx-alert">账号和角色由系统用户管理维护。此处仅设置访问业务资源所需的内部租户与业务部门。</a-alert>
      <a-form :model="profileForm" layout="vertical" @submit-success="saveProfile">
        <a-form-item field="tenantId" label="内部租户" required extra="已有外部身份绑定时，不允许直接变更内部租户。"><a-input v-model="profileForm.tenantId" placeholder="如：acme" /></a-form-item>
        <a-form-item field="departmentId" label="业务部门编号" extra="与系统组织部门分别维护，用于业务数据和知识资料的部门范围。"><a-input v-model="profileForm.departmentId" placeholder="如：sales" /></a-form-item>
        <a-alert v-if="error" type="error" class="nx-alert">{{ error }}</a-alert>
        <div class="nx-form-footer"><a-button @click="profileUser = null">取消</a-button><a-button type="primary" html-type="submit" :loading="busy">保存业务档案</a-button></div>
      </a-form>
    </a-modal>
    <a-modal v-model:visible="showIdentity" title="绑定外部身份" :footer="false" :mask-closable="false" :width="600" modal-class="novex-modal">
      <a-form :model="identityForm" layout="vertical" @submit-success="saveIdentity">
        <a-form-item field="userId" label="系统账号" required><a-select v-model="identityForm.userId" allow-search @change="identityUserChanged"><a-option v-for="user in enabledUsers" :key="user.id" :value="Number(user.id)">{{ user.username }} · {{ user.nickname }}</a-option></a-select></a-form-item>
        <a-form-item field="datasourceId" label="数据连接" required><a-select v-model="identityForm.datasourceId" @change="clearExternalIdentity"><a-option v-for="source in sources" :key="source.id" :value="source.id">{{ source.name }}</a-option></a-select></a-form-item>
        <div class="nx-form-grid"><a-form-item field="tenantId" label="内部租户编号" required><a-input :model-value="identityForm.tenantId" readonly placeholder="来自系统账号的业务档案" /></a-form-item><a-form-item field="employeeId" label="外部员工编号" required><a-input v-model="identityForm.employeeId" placeholder="源数据库中的员工编号" /></a-form-item></div>
        <a-form-item field="externalTenantId" label="外部租户 / 公司编号" :required="!sources.find((source) => source.id === identityForm.datasourceId)?.tenantId"><a-input v-model="identityForm.externalTenantId" placeholder="源数据库中的租户或公司值" /></a-form-item>
        <a-form-item field="departmentId" label="外部部门编号"><a-input v-model="identityForm.departmentId" placeholder="源数据库中的部门编号" /></a-form-item>
        <a-form-item field="warehouseText" label="仓库编号"><a-input v-model="identityForm.warehouseText" placeholder="多个编号以英文逗号分隔" /></a-form-item>
        <a-alert v-if="error" type="error" class="nx-alert">{{ error }}</a-alert>
        <div class="nx-form-footer"><a-button @click="showIdentity = false">取消</a-button><a-button html-type="submit" type="primary" :loading="busy">保存绑定</a-button></div>
      </a-form>
    </a-modal>
  </NovexPage>
</template>

<style scoped src="../novex.css"></style>
