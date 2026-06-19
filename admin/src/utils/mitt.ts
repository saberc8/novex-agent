import type { EventType } from 'mitt'
import mitt from 'mitt'

type Events = Record<EventType, any> & {
  // 自定义事件名称
  event: void
  // 任意传递的参数
  [params: string]: any
}

const mittBus = mitt<Events>()
export default mittBus
