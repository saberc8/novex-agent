# Novex Chat

独立的 React + TypeScript 对话应用，与 `admin/` 和 `client/` 分开构建、部署。界面只接触工作账号的会话凭证，不接收或保存模型 API key。

```sh
cd chat
corepack pnpm@8.15.9 install --ignore-scripts
corepack pnpm@8.15.9 dev
```

开发端口为 `8312`，`/api/novex` 代理到 `127.0.0.1:8310`。生产构建执行 `corepack pnpm@8.15.9 build`，产物在 `dist/`，由部署方代理同源 `/api/novex` 到后端。

可选环境变量：

- `VITE_API_BASE`：API 根地址，默认为 `/api/novex`。
- `VITE_EMBED_ALLOWED_ORIGINS`：逗号分隔的父页面 origin，默认允许 `http://localhost:8311` 和 `http://127.0.0.1:8311`。必须包含协议、主机及端口；不接受通配符。

## 嵌入

父页面使用 `<iframe src="http://localhost:8312/?embed=1&parentOrigin=http%3A%2F%2Flocalhost%3A8311">`。iframe URL 中没有登录 token。部署时让服务器 CSP `frame-ancestors` 与允许嵌入的父页面保持一致。

1. Chat 向指定且被允许的父 origin 发送 `{type: 'novex:ready'}`。
2. 父页面校验 `event.origin === chatOrigin` 以及 `event.source === iframe.contentWindow`，以当前登录身份调用 `POST /api/novex/embed-tickets`，请求体为 `{origin: parentOrigin}`。
3. 父页面使用明确的 `targetOrigin` 向 iframe 发送 `{type: 'novex:launch', ticket}`。
4. Chat 同时校验来源窗口、父 origin 和消息结构，再调用 `/embed-exchange`。票据一次有效，过期时间由服务端控制。HTTP Origin 是 Chat 自身的 origin，票据绑定的是父页面 origin，两者不能混同。
5. 成功后 Chat 回复 `{type: 'novex:authenticated'}`。嵌入模式凭证仅保存在内存；独立页面使用 sessionStorage 支持当前浏览器会话内刷新。

Vue 管理台的“对话助手”已实现完整的票据流程，可作为集成样例。服务端 `NOVEX_EMBED_PARENT_ORIGINS`（兼容 `NOVEX_EMBED_ORIGINS`）、`NOVEX_CHAT_ORIGINS`（兼容单个 `NOVEX_CHAT_ORIGIN`）与前端允许列表需要对应。前端来源校验不是权限来源，数据访问始终由后端重新授权。

## 交互

支持真实账号登录、历史对话、新建对话、Pi/DeepSeek Harness 切换、SSE 运行状态、取消、Markdown 回答、知识上传和资料阅读。权限改变后被冻结的会话提示用户重新开始。回答只在服务端完成发布检查后显示，不展示敏感 token 流。资料侧栏展示“可用资料”，不把未使用的文档冒充引用。
