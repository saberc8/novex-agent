<template>
  <a-modal
    v-model:visible="visible"
    :title="title"
    :mask-closable="false"
    :esc-to-close="false"
    :width="width >= 600 ? 600 : '100%'"
    draggable
    @before-ok="save"
    @close="reset"
  >
    <GiForm ref="formRef" v-model="form" :columns="columns" layout="vertical" />
  </a-modal>
</template>

<script setup lang="ts">
import type { ColumnItem } from '@/components/GiForm'
import { Message } from '@arco-design/web-vue'
import { useWindowSize } from '@vueuse/core'
import { h, resolveComponent } from 'vue'
import { addClient, getClient, updateClient } from '@/apis/system/client'
import { GiForm } from '@/components/GiForm'
import { useResetReactive } from '@/hooks'
import { useDict } from '@/hooks/app'

const emit = defineEmits<{
  (e: 'save-success'): void
}>()

const { width } = useWindowSize()

const dataId = ref('')
const visible = ref(false)
const isUpdate = computed(() => !!dataId.value)
const title = computed(() => (isUpdate.value ? '修改客户端' : '新增客户端'))
const formRef = ref<InstanceType<typeof GiForm>>()
const { client_type, auth_type_enum } = useDict('auth_type_enum', 'client_type')

const [form, resetForm] = useResetReactive({
  activeTimeout: 1800,
  timeout: 86400,
  isConcurrent: 1,
  isShare: 1,
  status: 1,
})

const columns: ColumnItem[] = reactive([
  {
    label: '客户端类型',
    field: 'clientType',
    type: 'select',
    span: 12,
    props: {
      options: client_type,
    },
  },
  {
    label: '认证类型',
    field: 'authType',
    type: 'select',
    required: true,
    span: 12,
    props: {
      options: auth_type_enum,
      multiple: true,
      maxTagCount: 2,
    },
  },
  {
    label: () => h(resolveComponent('a-tooltip'), { content: '-1 代表不限制，永不冻结' }, {
      default: () => ['Token 最低活跃频率', h(resolveComponent('icon-info-circle-fill'))],
    }),
    field: 'activeTimeout',
    type: 'input-number',
    span: 12,
    slots: {
      append: () => h('span', { style: { width: '30px', textAlign: 'center' } }, '秒'),
    },
    props: {
      placeholder: '请输入 Token 最低活跃频率',
    },
    rules: [{ required: true, message: '请输入 Token 最低活跃频率' }],
  },
  {
    label: () => h(resolveComponent('a-tooltip'), { content: '-1 代表永不过期' }, {
      default: () => ['Token 有效期', h(resolveComponent('icon-info-circle-fill'))],
    }),
    field: 'timeout',
    type: 'input-number',
    span: 12,
    slots: {
      append: () => h('span', { style: { width: '30px', textAlign: 'center' } }, '秒'),
    },
    props: {
      placeholder: '请输入 Token 有效期',
    },
    rules: [{ required: true, message: '请输入 Token 有效期' }],
  },
  {
    label: '状态',
    field: 'status',
    type: 'switch',
    span: 24,
    props: {
      type: 'round',
      checkedValue: 1,
      uncheckedValue: 2,
      checkedText: '启用',
      uncheckedText: '禁用',
    },
  },
])

// 重置
const reset = () => {
  formRef.value?.formRef?.resetFields()
  resetForm()
}

// 保存
const save = async () => {
  try {
    const isInvalid = await formRef.value?.formRef?.validate()
    if (isInvalid) return false
    if (isUpdate.value) {
      await updateClient(form, dataId.value)
      Message.success('修改成功')
    } else {
      await addClient(form)
      Message.success('新增成功')
    }
    emit('save-success')
    return true
  } catch {
    return false
  }
}

// 新增
const onAdd = async () => {
  reset()
  dataId.value = ''
  visible.value = true
}

// 修改
const onUpdate = async (id: string) => {
  reset()
  dataId.value = id
  const { data } = await getClient(id)
  Object.assign(form, data)
  visible.value = true
}

defineExpose({ onAdd, onUpdate })
</script>

<style scoped lang="scss">
:deep(.arco-input-append) {
  padding: 0;
  .arco-btn {
    border-top-left-radius: 0;
    border-bottom-left-radius: 0;
    border: 1px solid transparent;
  }
}
</style>
