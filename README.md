# Novex Agent

基于 `template-rbac-go` 的外部 Agent 企业知识与业务助手 Demo。

[源码](https://github.com/saberc8/novex-agent) · [Apache-2.0](LICENSE) · [第三方许可与来源](THIRD_PARTY_NOTICES.md)

Go 平台负责身份、外部 PostgreSQL 接入、权限、业务 MCP、知识文件及会话；Pi 和 DeepSeek Harness 负责各自的 Agent 循环。Chat 是独立 React 工程，可独立部署或通过一次性启动票据嵌入 iframe。当前版本没有 Codex CLI 依赖，也没有向量数据库。

仓库包含完整源码、合成样例和数据库生成脚本。本文及验证文档中的已建数据库、账号授权和性能数字来自本地验收环境；新克隆需要执行下方初始化步骤。仓库不附带运行中的数据库、账号密码、模型 API key 或私有验收日志。

## 已实现

- 主数据库 `novex-agent`，与 `novex-hr`、`novex-hardware` 两个业务数据库分离。
- 可视化配置 PostgreSQL 连接、测试只读凭证、扫描 schema、注册表与字段为业务资源。
- 系统用户与每个外部数据源分别绑定身份：内部租户、外部公司/租户 ID、员工 ID、部门 ID、仓库列表。
- 角色授权同时绑定资源、行范围与字段；支持本人、本部门、指定仓库、本租户，以及明文/脱敏字段。
- 所有数据查询通过同一个 Go 权限执行器；MCP、REST、权限预览使用相同规则。预览隐藏业务值。
- 真实 Pi 与 DeepSeek Harness，通过 `api.b.ai` 的 `deepseek-v4.1-flash`，每次运行使用独立非 root 只读容器。
- 主模型密钥只保留在宿主模型代理；Agent 获得单次运行的短期模型凭证与 MCP 凭证。
- MD、TXT、DOCX、文字 PDF 上传转 Markdown；按权限物化独立只读目录，由 Agent 原生文件工具检索。
- XLSX/UTF-8 CSV 保留原件，按工作表、列类型和原始行号建立结构化索引；支持关键词、精确条件、数值范围、分页与单元格引用，不转成 Markdown。
- 独立 Chat：真实登录、历史、运行/取消、资料与引用、上传、Pi/DSH 切换、桌面与移动布局。
- 管理台：外部数据源、资源、身份、工作账号、角色、授权矩阵、权限预览、知识 ACL、审计及嵌入聊天。

管理功能使用原 RBAC 管理台的 Arco 组件、主题与动态菜单。账号、密码、角色和角色分配统一由「系统管理」维护，Novex 只扩展业务租户/部门档案、外部身份和业务数据权限；独立部署的是 `chat/` 对话应用。

新增账号流程：在「系统管理 → 用户管理」创建账号并分配系统角色，再在左侧「智能业务 → 身份绑定」为该已有账号开通业务档案，显式指定业务租户与部门，最后绑定外部数据库身份。未开通账号不自动推断租户，不能登录 Chat。外部数据库员工/部门/仓库 ID 仍通过各数据源的身份映射维护。

## 本地入口

| 服务 | 地址 |
|---|---|
| 独立 Chat | http://localhost:8312 |
| 管理台 | http://localhost:8311，登录后展开左侧「智能业务」 |
| Go API | http://localhost:8310/api/novex |
| Agent supervisor | http://127.0.0.1:8320，仅内部控制 |
| 模型代理 | http://127.0.0.1:8330，仅内部模型能力 |

「智能业务」使用原 RBAC 左侧菜单，不再在同一个页面内堆叠页签。九个独立页面各自加载所需数据，并支持直接访问、刷新和系统顶部多页标签：

| 页面 | 路径 |
|---|---|
| 工作概览 | `/novex/console`（保留原入口） |
| 数据连接 | `/novex/datasources` |
| 业务资源 | `/novex/resources` |
| 身份绑定 | `/novex/identities` |
| 数据授权 | `/novex/grants` |
| 权限预览 | `/novex/preview` |
| 知识资料 | `/novex/knowledge` |
| 运行审计 | `/novex/audit` |
| 对话助手 | `/novex/chat` |

本地验收时，`admin` 显式配置了 `novex_super_admin`（Novex 全局数据管理员），可以跨企业读取全部知识资料（包含私有文件）与所有已注册业务资源、行和字段。这个权限来自独立角色，普通 `admin` 系统管理角色不会自动获得；`scale_admin` 和其他员工继续使用各自的权限。新安装的账号密码由使用者通过 `BOOTSTRAP_ADMIN_PASSWORD` 自行设置，Novex 种子脚本要求显式配置该密码。不要将模型密钥或数据库密码写入前端、URL、代码或公开文档。

全新初始化默认仍分离管理权限和数据权限。需要复现当前全局账号时，在基础初始化后显式执行 `python3 scripts/enable_global_admin.py`；该维护命令只给已有 `admin` 增加全局数据角色，保留账号密码与原角色，并记录审计。角色变更后应重新登录并新建对话。全局读取仍通过只读业务连接与已注册资源，不开放任意 SQL、数据库写入或其他用户的聊天历史。

| 账号 | 内部租户 | 业务身份与预置权限 |
|---|---|---|
| a | acme | E001；本人薪资、产品资料 |
| b | acme | E002；本人全部薪资、销售部有限字段、W1 仓库 |
| c | acme | E003；本租户薪资，银行字段脱敏 |
| d | acme | E004；本人薪资 |
| e | acme | E005；本人薪资、W2 仓库 |
| x | globex | 另一租户的 E001、W1，用于同编号隔离验证 |

业务库全部为合成数据。HR 含员工与工资单；硬件库含产品、仓库、库存、入出库流水。六个资源可从管理台调整或增加，不依赖模型生成任意 SQL。

另提供两套独立企业规模测试库的生成器，不覆盖上述小样例。本次生成结果：`novex-hr-scale` 为 **2.084 GiB / 9,110,420 行**，`novex-hardware-scale` 为 **2.302 GiB / 9,142,419 行**。容量按 PostgreSQL `pg_database_size` 计算，含业务表和索引。运行规模生成与注册脚本后，它们通过同一可视化数据源系统接入，包含 15 个资源和独立的 `scale_admin / scale_a / scale_b / scale_c / scale_x` 账号。账号密码仍来自 `BOOTSTRAP_ADMIN_PASSWORD`。

| 规模测试账号 | 场景 |
|---|---|
| scale_admin | SC001 管理配置、发布共享资料，无默认业务数据读取权 |
| scale_a | SC001 员工 E001，本人工资与公开产品/知识资料 |
| scale_b | SC001 部门主管 E002，本部门有限薪资字段、W1 仓库、订单与售后 |
| scale_c | SC001 的 HR 与采购角色，本租户业务数据及其私有采购工作簿 |
| scale_x | SC002，具有相同 E001 / W1 编号，用于跨企业隔离验证 |

规模场景另有 80 份企业 Markdown 资料、含 50,000 行历史报价的 XLSX，以及带 48 个已保存公式结果的私有采购 XLSX。A/B 可见 65 份、C 可见 82 份、X 看不到 SC001 的这些文件。详细表结构、事实、完整性校验与压力结果见 [规模数据文档](docs/scale-data.md)；Excel 检索机制与限制见 [文件检索说明](docs/file-search.md)。

## 首次配置

需要 Go 1.24+、Docker、PostgreSQL（含 `psql` 和 `pg_trgm`）、Node 24/npm、Corepack、Python 3 和 OpenSSL。PostgreSQL 由用户提供，基础初始化创建三个 `novex-*` 数据库及 `novex_*` 角色；可选规模生成器再创建两个 `*-scale` 数据库及专用只读角色，不修改其他项目数据库。Redis 使用单独的 Compose 卷和端口。

```sh
git clone https://github.com/saberc8/novex-agent.git
cd novex-agent

# 项目私有 Python 环境；后续 python3 命令使用这个虚拟环境
python3 -m venv .local/python
source .local/python/bin/activate
python3 -m pip install -r scripts/dev-requirements.txt

# 交互式输入模型 key 与 Demo 账号密码；生成随机数据库密码、JWT、RSA、加密密钥
python3 scripts/configure_demo.py

# 项目内 Node 24 与两种 harness（不全局安装）
cd agent-worker && npm ci && cd ..
corepack pnpm --dir chat install --frozen-lockfile
corepack pnpm --dir admin install --frozen-lockfile

docker compose -p novex -f deploy/compose.yml up -d
docker build -t novex-agent-runtime:local agent-worker
```

通过 `PGHOST`、`PGPORT`、`PGUSER`、`PGPASSWORD` 提供本地 PostgreSQL 管理连接；也可使用忽略版本控制的 `.local/bootstrap.json`（host/port/user/password/dbname）。不要将这些凭证提交到 Git。

```sh
python3 scripts/bootstrap_databases.py
python3 scripts/seed_demo.py
python3 scripts/dev.py start all
python3 scripts/dev.py status
```

`.env.local` 保持私有；数据源密码入主库前使用 AES-GCM 加密，解密密钥不在数据库内。后续启动可直接执行 `scripts/dev.py start all`。`restart api` 会重新编译后端，`restart worker` 重新加载 supervisor；修改 harness 源码后还需重新构建 runtime 镜像。

如果模型服务需要网络代理，在 `.env.local` 设置 `NOVEX_HTTPS_PROXY`。此代理只用于宿主模型网关，业务 MCP 和本地 API 不经过该代理。不要将宿主代理的 `127.0.0.1` 地址直接当作容器内地址。

开发进程日志和 PID 位于 `.local/`。停止应用用 `python3 scripts/dev.py stop all`；停止专用 Redis 用 `docker compose -p novex -f deploy/compose.yml stop`，数据卷保留。

生产构建的 Admin 默认向同源地址请求。部署时将 `/auth`、`/system`、`/api/novex` 等后端路径反向代理到自己的 Go 服务，并配置自己的 RSA 公钥、CORS 与 iframe 来源。模板第三方演示站不作为生产 API 默认目标。

## 可验证场景

- 用 a 提问：「我 2026 年 10 月的应发工资多少，何时发放？」应发 9,000，发薪规则来自授权知识文件。
- 用 b 提问：「HW-002 在我的仓库还有多少可用库存，售价和保修期是什么？」W1 库存 42，预留 10，可用 32，售价 29，保修 12 个月。
- 用 b 查 E004 工资：可查看基本工资和应发合计，不能查看奖金。
- 用 b/e 查询出入库流水：各自只见 W1/W2；x 的同名 W1 属于另一租户。
- 在管理台新增只读 PostgreSQL 连接，扫描并注册新资源，再绑定账号外部身份和角色授权。
- 撤销授权后，后续查询被拒绝，在途运行取消，旧 Agent 会话冻结；不能从旧模型上下文继续读出已撤销内容。

企业规模数据可以在独立 Chat 中用以下问题试用（登录 `scale_*` 账号）：

| 账号 | 提问 | 核对重点 |
|---|---|---|
| scale_a | 我 2026 年 10 月应发工资多少，什么时候发？再帮我查 E002 的工资。 | 本人应发 9,000；每月 10 日发上月工资；他人薪资不可见 |
| scale_b | W1 的 HW-000002 够发 30 件吗？15 点下单能保证今天发货吗？ | 可用 32，分配后剩 2；还需审核、付款核验与分配，不直接保证发货 |
| scale_a | Excel 中 HW-002499 的 2026-11 报价是多少？比 2026-10 涨了多少？ | 888.25 对比 888.15，涨 0.10；引用各月的原始 Excel 行 |
| scale_b | 查 AS-ANCHOR-W1 售后状态，签收 10 天的质量问题还在退货申请窗口吗？ | 真实工单状态结合 15 天申请窗口；不宣称已经退款 |
| scale_c | 私有采购表 HW-000001 的含税单价和 120 包行金额是多少？ | 从保存的公式缓存读 42.94 和 5,152.80，引用 Procurement 第 2 行 |
| scale_x | 我公司的 W1 仓 HW-000001 有多少库存，现价多少？ | 自己企业库存 9,999、售价 199；不能借用 SC001 的同名记录 |

## 验证命令

```sh
# 无模型费用：真实本地HTTP鉴权/字段/租户/仓库/iframe票据
python3 scripts/check_api.py
# 原RBAC创建账号→显式开通业务档案→Chat登录与外部身份；清理测试账号，会改变策略版本
python3 scripts/check_rbac.py
# 仅核对九个独立菜单与管理权限，不创建测试账号
python3 scripts/check_rbac.py --menus-only
# 无模型费用：四种文档转换与跨用户拒绝读取；会改变策略版本
python3 scripts/check_documents.py
# 会调用真实模型：两种外部harness连接真实HR/WMS和文件知识库
python3 scripts/check_agents.py
# 真实启动并取消Docker运行，验证答案不发布与容器清理
python3 scripts/check_cancel.py

# 企业规模数据：可续建，不覆盖原有小样例；之后注册独立账号/数据源
python3 scripts/scale_build.py --database both
python3 scripts/scale_register.py
# 持续的授权业务 API 压力；各并发档至少30秒，不调用模型
python3 scripts/scale_benchmark.py --api-only --duration 30
# XLSX/CSV 功能与权限（临时上传会改变策略版本），以及独立只读压力
python3 scripts/check_spreadsheets.py --mode functional
python3 scripts/check_spreadsheets.py --mode benchmark --duration-seconds 15
# 82份企业知识样本；先完成全部资料/授权修改，再开始模型对话
python3 scripts/check_enterprise.py --prepare
python3 scripts/check_enterprise.py --oracle-only
# 真实付费模型：Pi 8场景、DSH 8场景，含3组多轮对话
python3 scripts/check_enterprise.py --run

# Go单元测试；数据库集成测试另需加载私有配置并设置NOVEX_INTEGRATION=1
cd backend && go test ./...
# runtime边界测试
cd ../agent-worker && npm test
# 固定模型代理的能力约束/撤销测试
cd .. && agent-worker/node_modules/node/bin/node --test scripts/model-proxy.test.mjs
```

动态数据库配置集成测试在 `backend/internal/novex/configuration_integration_test.go`：从 HTTP 新建数据源一直走到身份映射与真实查询；新账号的内部租户与外部公司 ID 不同。另有可控 runner 的撤权竞态测试：先通过真实 MCP 读到薪资，再撤权，确保迟到答案无法发布。测试会提升全局权限版本，使已有会话冻结，因此应在开始交互演示前运行。

## 权限与当前边界

权限由服务端强制执行，提示词与 Skills 不承担授权职责。外部库连接必须是只读账号，查询只使用已注册表/列与参数化过滤；Agent 不持有数据库连接，也没有任意 SQL、Shell 或写文件工具。

业务查询要求账号外部身份映射。共享业务库通过外部租户列过滤；没有租户列的独立库必须显式绑定到一个内部租户。一个字段只能由同时覆盖该行的授权返回，不能把不同角色的行范围与字段范围拼接成扩大权限。

显式 `novex_super_admin` 角色是上述业务行/字段与文件 ACL 的全局读取例外，覆盖当前和以后注册的业务资源及上传资料。此角色无需伪造某个员工身份，返回数据保留企业列以区分同编号记录；“我的工资”未指定员工身份时，Agent 应先确认企业和员工。全局数据读取能力不隐含新增管理接口或业务写入权限。

此版本采用保守的全局权限版本：任何权限相关变更都可能冻结旧会话。模型结果缓冲，在发布前重新验证。已合法发给用户的内容不能追回。管理 API 及旧模板角色写入口还受授权治理角色限制；通用系统日志不收集 Novex 请求/响应正文。

文字类知识文件持久化 Markdown 与 ACL，原上传文件仅用于临时转换；XLSX/CSV 则保留原件并提供经鉴权的下载，工作表和数据行保存在 PostgreSQL 结构化索引中。没有文档版本历史。扫描 PDF/OCR、复杂版面恢复尚未实现，转换失败会明确返回错误。PDF 文本带页码标题，DOCX 保留标题层级与表格。

Excel 权限作用于整份文件及其全部工作表；需要按员工、部门或字段隔离时，应接入业务数据库配置数据授权。公式只读取保存的缓存值，缺失时明确提示，不执行公式、宏或外部链接。当前支持 `.xlsx` 与 UTF-8 `.csv`，不支持 `.xls`/`.xlsm`。

容器已隔离文件与进程，但尚未增加独立的网络出口防火墙；当前可见工具被限制为只读文件及指定业务 MCP，模型代理固定上游与模型。DSH 是固定版本的 developer preview。此版本提供只读业务问答，未实现业务写回审批或任意 SQL/跨库 JOIN。

## 目录与文档

- `backend/internal/novex/`：业务、权限、MCP、知识、会话；`backend/cmd/novex-seed/`：幂等种子。
- `agent-worker/`：外部 harness 适配与每次运行容器。[运行说明](agent-worker/README.md)
- `chat/`：独立 Chat。[iframe 接入说明](chat/README.md)
- `admin/src/views/novex/`：管理台 Novex 页面。
- `scripts/`：本地配置、数据库初始化、进程管理、固定模型代理与验收脚本。
- [API 契约](docs/novex-api.md)、[文件检索说明](docs/file-search.md)、[企业规模数据与压测](docs/scale-data.md)、[验证记录及已知范围](docs/verification.md)、[原 RBAC 模板说明](docs/template-rbac-go.md)。

本项目采用 [Apache License 2.0](LICENSE)。底座来自 [saberc8/template-rbac-go](https://github.com/saberc8/template-rbac-go)；管理台保留 [ContiNew Admin UI 的原始许可](admin/LICENSE)。第三方组件继续适用各自的许可，详见 [NOTICE](NOTICE) 和 [第三方许可说明](THIRD_PARTY_NOTICES.md)。
