import { cloneDeep } from 'lodash-es'
import { reactive } from 'vue'

export function useResetReactive<T extends Record<string, any>>(value: T) {
  const getInitValue = () => cloneDeep(value)

  const state = reactive(getInitValue()) as T & Record<string, any>

  const reset = () => {
    Object.keys(state).forEach((key) => delete state[key])
    Object.assign(state, getInitValue())
  }

  return [state, reset] as const
}
