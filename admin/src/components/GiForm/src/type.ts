import type * as A from '@arco-design/web-vue'
import type { VNode } from 'vue'

export type ColumnItemType
  = | 'input'
    | 'input-password'
    | 'input-number'
    | 'input-tag'
    | 'textarea'
    | 'select'
    | 'tree-select'
    | 'radio-group'
    | 'checkbox-group'
    | 'date-picker'
    | 'year-picker'
    | 'quarter-picker'
    | 'month-picker'
    | 'week-picker'
    | 'time-picker'
    | 'range-picker'
    | 'color-picker'
    | 'rate'
    | 'switch'
    | 'slider'
    | 'cascader'
    | 'upload'
    | 'auto-complete'
    | 'mention'
    | 'group-title'

export type ColumnItemOptions
  = | A.SelectInstance['$props']['options']
    | A.RadioGroupInstance['$props']['options']
    | A.CheckboxGroupInstance['$props']['options']
    | A.CascaderInstance['$props']['options']

export type ColumnItemData
  = | A.TreeSelectInstance['$props']['data']
    | A.AutoCompleteInstance['$props']['data']
    | A.MentionInstance['$props']['data']

interface AutoCompleteSlots {
  option: (e: { data: (string | number | A.SelectOptionData | A.SelectOptionGroup)[] }) => VNode
  footer: () => VNode
}

interface CascaderSlots {
  'label': (e: { data: A.CascaderOption }) => VNode
  'prefix': () => VNode
  'arrow-icon': () => VNode
  'loading-icon': () => VNode
  'search-icon': () => VNode
  'empty': () => VNode
  'option': (e: { data: A.CascaderOption }) => VNode
}

interface CheckboxGroupSlots {
  checkbox: (e: { checked: boolean, disabled: string }) => VNode
  label: (e: { data: A.CheckboxOption }) => VNode
}

interface RadioGroupSlots {
  radio: (e: { checked: boolean, disabled: string }) => VNode
  label: (e: { data: any }) => VNode
}

interface DatePickerSlots {
  'prefix': () => VNode
  'suffix-icon': () => VNode
  'icon-next-double': () => VNode
  'icon-prev-double': () => VNode
  'icon-next': () => VNode
  'icon-prev': () => VNode
  'cell': (e: { data: Date }) => VNode
  'extra': () => VNode
}

interface InputSlots {
  append: (() => VNode) | string
  prepend: (() => VNode) | string
  suffix: (() => VNode) | string
  prefix: (() => VNode) | string
}

interface InputNumberSlots {
  minus: (() => VNode) | string
  plus: (() => VNode) | string
  append: (() => VNode) | string
  prepend: (() => VNode) | string
  suffix: (() => VNode) | string
}

interface InputTagSlots {
  tag: (e: { data: A.TagData }) => VNode
  prefix: (() => VNode) | string
  suffix: (() => VNode) | string
}

interface RateSlots {
  character: (e: { index: number }) => VNode
}

interface SelectSlots {
  'trigger': () => VNode
  'prefix': () => VNode
  'search-icon': () => VNode
  'loading-icon': () => VNode
  'arrow-icon': () => VNode
  'footer': () => VNode
  'header': () => VNode
  'label': (e: { data: A.SelectOptionData }) => VNode
  'option': (e: { data: A.SelectOptionData }) => VNode
  'empty': () => VNode
}

interface SwitchSlots {
  'checked-icon': () => VNode
  'unchecked-icon': () => VNode
  'checked': () => VNode
  'unchecked': () => VNode
}

interface TreeSelectSlots {
  'trigger': () => VNode
  'prefix': () => VNode
  'label': (e: { data: any }) => VNode
  'header': () => VNode
  'loader': () => VNode
  'empty': () => VNode
  'footer': () => VNode
  'tree-slot-extra': () => VNode
  'tree-slot-title': (e: { title: string }) => VNode
  'tree-slot-icon': (e: { node: A.TreeNodeData }) => VNode
  'tree-slot-switcher-icon': () => VNode
}

interface MentionSlots {
  option: (e: { data: any }) => VNode
}

export type ComponentSlots
  = & AutoCompleteSlots
    & CascaderSlots
    & CheckboxGroupSlots
    & RadioGroupSlots
    & DatePickerSlots
    & InputSlots
    & InputNumberSlots
    & InputTagSlots
    & RateSlots
    & SelectSlots
    & SwitchSlots
    & TreeSelectSlots
    & MentionSlots

export interface ColumnItemSlots extends Omit<ComponentSlots, 'label' | 'option'> {
  label?: (e: { data: A.CheckboxOption | A.SelectOptionData | A.CascaderOption }) => VNode
  option?: (e: { data: (string | number | A.SelectOptionData | A.SelectOptionGroup)[] | A.CascaderOption | A.SelectOptionData }) => VNode
}

export type ColumnItemHide<F> = boolean | ((form: F) => boolean)
export type ColumnItemShow<F> = boolean | ((form: F) => boolean)
export type ColumnItemDisabled<F> = boolean | ((form: F) => boolean)
export type ColumnItemRequest<F = any> = (form: F) => Promise<any>
export type ColumnItemFormat<T = any> = (res: T) => ColumnItemOptions | ColumnItemData

export interface ColumnItem<F = any> {
  type?: ColumnItemType // 类型
  label?: A.FormItemInstance['label'] | (() => VNode) // 标签
  field: A.FormItemInstance['field'] // 字段(必须唯一)
  span?: GridSpan
  props?: Record<string, any>
  gridItemProps?: Record<string, any>
  formItemProps?: Record<string, any> // a-form-item的props
  required?: any // 是否必填
  rules?: any // 表单校验规则
  hide?: any // 是否隐藏
  show?: any // 是否显示（优先级比hide高）
  disabled?: any // 是否禁用
  request?: ColumnItemRequest<F> // 接口请求api
  resultFormat?: ColumnItemFormat // 结果集格式化
  init?: boolean // 初始化请求
  cascader?: string[] // 级联的field字段列表
  slots?: Partial<ColumnItemSlots>
  formItemSlots?: Partial<Record<'help' | 'extra', string | (() => VNode)>>
}

type GridSpan = number | Partial<Record<'xs' | 'sm' | 'md' | 'lg' | 'xl' | 'xxl', number>>
