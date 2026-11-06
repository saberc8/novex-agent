<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { novex } from '@/apis/novex'
import NovexPage from '../shared/NovexPage.vue'
import { usePageLoad, usePageState } from '../shared/page'

defineOptions({ name: 'NovexConsole' })
const { error, success, loading, load } = usePageState()
const router = useRouter()
const overview = ref<Record<string, number>>({})
const chatBase
  = import.meta.env.VITE_NOVEX_CHAT_URL
    || `${location.protocol}//${location.hostname}:8312`
async function refresh() {
  await load(async () => {
    overview.value = await novex<Record<string, number>>('/admin/overview')
  })
}
usePageLoad(refresh)
</script>

<template>
  <NovexPage title="工作空间概览" :loading="loading" :error="error" :success="success" @refresh="refresh" @dismiss-error="error = ''" @dismiss-success="success = ''">
    <template #actions><a-tag>权限版本 {{ overview.policyVersion ?? '—' }}</a-tag><a-button type="primary" :href="chatBase" target="_blank" rel="noopener">打开独立对话 <icon-launch /></a-button></template>
    <a-alert class="nx-alert">账号、角色和菜单权限统一使用系统 RBAC。在此配置外部数据、业务身份和数据授权；对话应用可独立部署或嵌入。</a-alert>
    <div class="nx-stats">
      <a-card
        v-for="item in [
          { key: 'datasources', label: '数据连接', tab: 'sources' },
          { key: 'resources', label: '业务资源', tab: 'resources' },
          { key: 'documents', label: '知识资料', tab: 'knowledge' },
          { key: 'users', label: '已开通账号', tab: 'identities' },
          { key: 'runs', label: '运行记录', tab: 'audit' },
        ]" :key="item.key" size="small"
      >
        <a-statistic :title="item.label" :value="overview[item.key] ?? 0" />
        <a-button type="text" size="small" @click="router.push(`/novex/${item.tab === 'sources' ? 'datasources' : item.tab}`)">查看详情 <icon-right /></a-button>
      </a-card>
    </div>
    <div class="nx-overview-grid">
      <a-card title="接入流程" size="small">
        <a-list :bordered="false">
          <a-list-item
            v-for="(step, index) in [
              { title: '连接 PostgreSQL', text: '测试只读连接，扫描数据表与字段。', tab: 'sources' },
              { title: '注册业务资源', text: '配置表、开放字段与租户、员工、部门、仓库列。', tab: 'resources' },
              { title: '绑定系统用户的外部身份', text: '开通业务档案，再关联每个业务库中的员工与组织。', tab: 'identities' },
              { title: '为系统角色配置数据授权', text: '设置可访问的行与字段，使用权限预览核验。', tab: 'grants' },
            ]" :key="step.tab"
          >
            <a-list-item-meta :title="step.title" :description="step.text"><template #avatar><a-avatar :size="32">{{ index + 1 }}</a-avatar></template></a-list-item-meta>
            <template #actions><a-button type="text" @click="router.push(`/novex/${step.tab === 'sources' ? 'datasources' : step.tab}`)">配置 <icon-right /></a-button></template>
          </a-list-item>
        </a-list>
      </a-card>
      <a-card title="统一权限管理" size="small">
        <a-descriptions :column="1" size="large">
          <a-descriptions-item label="系统用户">统一登录账号、状态、密码与角色分配</a-descriptions-item>
          <a-descriptions-item label="系统角色">统一菜单权限，并关联业务数据授权</a-descriptions-item>
          <a-descriptions-item label="业务身份">明确每个数据源中的租户、员工、部门与仓库</a-descriptions-item>
          <a-descriptions-item label="数据授权">同一条规则同时约束行范围和可见字段</a-descriptions-item>
        </a-descriptions>
        <a-space wrap>
          <a-button @click="router.push('/system/user')">系统用户管理</a-button>
          <a-button @click="router.push('/system/role')">系统角色管理</a-button>
          <a-button type="primary" @click="router.push('/novex/preview')">权限预览</a-button>
        </a-space>
      </a-card>
    </div>
  </NovexPage>
</template>

<style scoped src="../novex.css"></style>
