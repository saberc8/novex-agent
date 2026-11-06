import { Modal } from '@arco-design/web-vue'
import { onActivated, onMounted, ref } from 'vue'

export const scopeNames: Record<string, string> = {
  self: '仅本人',
  department: '本部门',
  warehouse: '指定仓库',
  tenant: '本企业',
}
export const display = (value: unknown) => value == null ? '—' : typeof value === 'object' ? JSON.stringify(value) : String(value)
export const when = (value: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—'
export function confirmDelete(content: string) {
  return new Promise<boolean>((resolve) => {
    Modal.confirm({ title: '确认操作', content, okText: '确认', cancelText: '取消', onOk: () => resolve(true), onCancel: () => resolve(false) })
  })
}

// State is created independently by each route; this helper holds no business data.
export function usePageState() {
  const error = ref('')
  const success = ref('')
  const busy = ref(false)
  const loading = ref(false)
  async function load(task: () => Promise<void>) {
    if (loading.value) return
    loading.value = true
    error.value = ''
    try {
      await task()
    } catch (e) {
      error.value = e instanceof Error ? e.message : '加载失败'
    } finally {
      loading.value = false
    }
  }
  async function action(task: () => Promise<void>, message = '') {
    if (busy.value) return
    busy.value = true
    error.value = ''
    success.value = ''
    try {
      await task()
      if (message) success.value = message
    } catch (e) {
      error.value = e instanceof Error ? e.message : '操作失败'
    } finally {
      busy.value = false
    }
  }
  return { error, success, busy, loading, load, action }
}

export function usePageLoad(refresh: () => Promise<void>) {
  let activated = false
  onMounted(refresh)
  onActivated(() => {
    if (activated) void refresh()
    activated = true
  })
}
