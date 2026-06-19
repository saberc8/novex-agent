import { viteMockServe } from 'vite-plugin-mock'

export default function createMock(env, isBuild) {
  const { VITE_BUILD_MOCK } = env
  return viteMockServe({
    mockPath: 'src/mock', // 目录位置
    logger: !isBuild, //  是否在控制台显示请求日志
    enable: !isBuild || VITE_BUILD_MOCK === 'true',
  })
}
