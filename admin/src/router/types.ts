import type { RouteRecordRaw } from 'vue-router'

export type AppRouteRecordRaw = Omit<RouteRecordRaw, 'children' | 'redirect'> & {
  children?: AppRouteRecordRaw[]
  redirect?: any
  meta?: RouteRecordRaw['meta'] & Record<string, any>
}
