# Novex 企业规模数据与压测

两套数据库均为独立的合成业务库，保留 `novex-hr`、`novex-hardware` 小样例库不变。本次实测环境为 `.local/bootstrap.json` 指向的 localhost:5432 PostgreSQL 16.3（Postgres.app），未操作本机另一个 PostgreSQL。新安装可以使用自己的 PostgreSQL 地址；仓库提供生成器，不包含这两套数据库文件。

容量验收使用 `pg_database_size`，每库最低 **2 × 1024³ = 2,147,483,648 字节**，包含真实业务表、主键和查询索引，不通过无业务大对象或占位文件填充。生成器以单连接串行加载，每批事务同时保存进度，重复运行可续建，不删除其他数据库。所有个人资料、合同、银行账号、地址与业务记录均为明确的合成数据。

## 数据结构

HR：20 家企业、每家 20 个部门、每家 5,000 名员工。表为 `companies`、`departments`、`employees`、`contracts`、`payroll`、`attendance`、`leave_requests`。每员工 1 份有效合同与 12 月工资（2025-11 至 2026-10）；考勤从 2026-11-05 向过去按完整 7 天、全部 10 万员工成批追加。请假与相应考勤状态关联；工资满足 `total=base+bonus+overtime_pay` 和 `net_pay=total-social_insurance-tax`。税费仅是用于校验的合成算式，不代表真实税务规则。

硬件/WMS：20 家企业、每家 2,500 SKU、5 个仓库，每仓 100 个库位。表为 `companies`、`products`、`warehouses`、`locations`、`inventory`、`orders`、`order_items`、`stock_movements`、`after_sales`。每个生成批次创建真实采购单和销售单、每单最多 5 行、逐行对应收发货流水；库存随相应流水变化，库存余额与全量流水净额对账。已分配未出库订单与 `reserved` 对账，售后工单关联订单行。三个锚点 SKU 不参与扩容，以保持稳定的回归事实。

两库均有复合业务主键（包含 `tenant_id`）、针对本人/部门/仓库及日期的索引，并在批量生成后添加、全量验证外键，最后执行 `ANALYZE`。20 家企业刻意共享 `E001` 等员工编号及 `W1` 等仓库编号，用来验证跨租户相同业务标识仍被隔离。

## 固定回归事实

| 租户 / 对象 | 事实 |
|---|---|
| SC001 / E001 / 2026-10 | base 8,000；bonus 1,000；total 9,000；net_pay 8,104；计划 2026-11-10 发放 |
| SC001 / E002 / 2026-10 | base 15,000；bonus 3,000；total 18,000 |
| SC001 / E5000 / 2026-10 | base 20,000；bonus 5,000；total 25,000 |
| SC001 / W1 / HW-000001 | 不锈钢六角螺栓 M8×30；售价 68；采购价 38；stock 800；reserved 120 |
| SC001 / W1 / HW-000002 | 工业轴承 6204-2RS；售价 29；采购价 16；stock 42；reserved 10；可用 32 |
| SC001 / W1 / HW-000003 | 电动扭矩扳手 18V；售价 899；采购价 520；stock 25；reserved 3 |
| SC002 / W1 / HW-000001 | 同名仓库与 SKU，但 stock 9,999；售价 199；采购价 99 |
| SC001 / W1 / HW-000002 收发货 | `PO-ANCHOR-W1` 入库 60，`SO-ANCHOR-W1` 出库 18，净额 42；待发货单 `SO-RESERVE-W1` 占用 10 |
| SC001 / W1 / AS-ANCHOR-W1 | HW-000003 电池无法充电，状态 open，未退货或改变库存，保修到 2027-11-05 |

所有产品保修 12 个月。对 SKU 编号 n≥4，售价为 `15+(n%500)*1.75`，采购价为售价 × 0.58 四舍五入到两位，产品名 `工业备件-<4位n>`。资料中的历史报价与数据库当前价分别标注，不能混当实时售价。

## 独立账号与资源

账号密码引用本地私有 `.env.local` 的 `BOOTSTRAP_ADMIN_PASSWORD`，不复制到本文件。

- `scale_admin`：SC001 的系统管理与授权治理角色，用于发布本租户知识资料；没有默认业务数据 grant。
- `scale_a`：SC001 / E001 / D001，只读本人薪资、合同、考勤和请假；可读本租户产品公开信息。
- `scale_b`：SC001 / E002 / D001，部门薪资仅基本工资和总额，自己的奖金由 self grant 读取；仓库范围 W1。
- `scale_c`：SC001 / E5000 / D020，明确拥有 HR 薪资与采购审计两个业务角色，银行信息脱敏；可读采购价格及本租户仓库数据。
- `scale_x`：SC002 / E001 / D001，本人资料及自身企业 W1，用于隔离验证。

资源 key：`scale_hr_companies`、`scale_hr_departments`、`scale_hr_employees`、`scale_hr_contracts`、`scale_hr_payroll`、`scale_hr_attendance`、`scale_hr_leave_requests`，以及 `scale_wms_products`、`scale_wms_warehouses`、`scale_wms_locations`、`scale_wms_inventory`、`scale_wms_orders`、`scale_wms_order_items`、`scale_wms_stock_movements`、`scale_wms_after_sales`。真实资源 ID、列清单和账号 ID 记录在 `.local/scale/registration.json`。

B 等仓库角色不获得采购单金额、订单行单价/金额，否则即使隐藏 `products.cost_price` 也会从采购订单推算成本。C 的采购审计角色显式授予这些字段。所有资源仍使用原有服务器端租户、外部身份、同一 grant 的行/字段耦合和隐藏字段筛选保护。

## 复现与证据

```sh
# 系统 Python 需 psycopg 3；读取私有 bootstrap 配置，无需在命令行传密码。
python3 scripts/scale_build.py --database both

# 业务注册集中在真实 Agent 回归前执行，避免过程中变更 policyVersion。
python3 scripts/scale_register.py

# 只读 SQL 与真实受控业务 API，1/4/8 并发，每档180请求。
python3 scripts/scale_benchmark.py --requests 180
```

新增连接环境变量前缀是 `NOVEX_HR_SCALE_`、`NOVEX_HARDWARE_SCALE_`，各自包含 HOST / PORT / DATABASE / USER / PASSWORD。角色分别为 `novex_hr_scale_read` 与 `novex_hardware_scale_read`，禁止超管、建库、建角色和绕过 RLS，默认只读且 statement timeout 8 秒；API 查询仍有自身的 5 秒数据库超时和最多 100 行限制。

证据文件：`.local/scale-build.log`、`.local/scale/hr-build.json`、`.local/scale/hardware-build.json`、`.local/scale/build-manifest.json`、`.local/scale/registration.json`、`.local/scale/benchmark.json`。生成报告包含实际字节数、逐表行数、堆/索引大小、外键状态、全量业务完整性检查及耗时。压测是本机热缓存读测试，包括连接建立和服务端授权开销；它不代表生产集群容量，也不包含模型响应时延。

## 本次实测结果



| 数据库 | pg_database_size 字节 | GiB | 业务总行数 | 单次完整构建耗时 |
|---|---:|---:|---:|---:|
| novex-hr-scale | 2,237,293,071 | 2.084 | 9,110,420 | 199.190 秒 |
| novex-hardware-scale | 2,472,288,783 | 2.302 | 9,142,419 | 328.735 秒 |

HR 逐表行数：`companies` 20；`departments` 400；`employees` 100,000；`contracts` 100,000；`payroll` 1,200,000；`attendance` 7,700,000；`leave_requests` 10,000。

硬件/WMS 逐表行数：`companies` 20；`products` 50,000；`warehouses` 100；`locations` 10,000；`inventory` 250,000；`orders` 800,300；`order_items` 3,996,099；`stock_movements` 3,995,800；`after_sales` 40,100。

两库业务合计 **18,252,839 行**。HR 考勤窗口为 2026-08-21 至 2026-11-05，共77天；硬件生成8个完整订单/流水批次。HR 8个、硬件11个外键均已VALIDATE。以下全量业务校验的违规数均为0：

- novex-hr-scale / `employee_department_mismatch`：0，检查耗时 0.016 秒。
- novex-hr-scale / `payroll_employee_or_department_mismatch`：0，检查耗时 1.530 秒。
- novex-hr-scale / `payroll_calculation_mismatch`：0，检查耗时 0.433 秒。
- novex-hr-scale / `attendance_employee_department_mismatch`：0，检查耗时 3.023 秒。
- novex-hr-scale / `leave_attendance_mismatch`：0，检查耗时 0.110 秒。
- novex-hr-scale / `unvalidated_foreign_keys`：0。
- novex-hardware-scale / `order_line_amount_mismatch`：0，检查耗时 0.685 秒。
- novex-hardware-scale / `order_total_mismatch`：0，检查耗时 6.010 秒。
- novex-hardware-scale / `movement_order_line_mismatch`：0，检查耗时 3.988 秒。
- novex-hardware-scale / `inventory_ledger_mismatch`：0，检查耗时 1.518 秒。
- novex-hardware-scale / `reserved_order_mismatch`：0，检查耗时 0.125 秒。
- novex-hardware-scale / `invalid_inventory`：0，检查耗时 0.029 秒。
- novex-hardware-scale / `unvalidated_foreign_keys`：0。

压测在大文件上传暂停期间单独执行。直接 SQL 和实际 Go API 各540请求，每档180，合计1,080请求。API每档含150次应放行查询与30次应拒绝的采购成本越权请求。所有状态、工资和库存/租户 oracle 通过。

| 层 | 并发 | 请求数 / 成功检查 | P50 ms | P95 ms | P99 ms | 请求/秒 |
|---|---:|---:|---:|---:|---:|---:|
| SQL | 1 | 180 / 180 | 0.136 | 0.311 | 15.527 | 2689.56 |
| SQL | 4 | 180 / 180 | 0.361 | 0.721 | 22.649 | 3061.43 |
| SQL | 8 | 180 / 180 | 0.829 | 23.750 | 28.844 | 2524.19 |
| API | 1 | 180 / 180 | 22.230 | 41.200 | 62.715 | 44.72 |
| API | 4 | 180 / 180 | 27.404 | 41.380 | 49.509 | 149.10 |
| API | 8 | 180 / 180 | 32.252 | 55.372 | 68.672 | 235.24 |

API三个并发档的权限误放率与误拒率均为0。SQL高百分位包含每个worker第一次建连接的开销；本轮是有界本地读压测，不是长时间稳态、冷缓存、跨地域或数据库写入压测。

代表性 EXPLAIN ANALYZE 结果：

| 查询 | 执行 ms | 使用索引 |
|---|---:|---|
| hr_self_payroll | 0.565 | payroll_pkey |
| hr_department_attendance | 0.538 | attendance_department_date_idx |
| hr_employee_leave | 0.333 | leave_employee_date_idx |
| wms_inventory_anchor | 0.051 | inventory_pkey |
| wms_latest_movements | 0.062 | stock_movements_sku_date_idx |
| wms_orders_page | 0.730 | orders_warehouse_date_idx |

完整执行计划及缓冲区命中记录在 `.local/scale/explain-analyze.json`。本次没有为凑容量写入无业务大blob：最大的表是考勤、订单明细、出入库流水及支持授权查询的索引。

## 持续 API 压力测试

完成上述有限批次测试后，另在独占测试窗口对真实 Go `/api/novex/query` 执行每档30秒的持续请求，依次使用1、4、8并发。大文件上传、Excel压力和真实Agent回归在此期间暂停，没有用模拟服务替代业务API。

```sh
python3 scripts/scale_benchmark.py --api-only --duration 30
```

| 并发 | 实际持续秒 | 请求数 / 预期通过 | 正常200 | 正确拒绝403 | P50 ms | P95 ms | P99 ms | 请求/秒 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| 1 | 30.004 | 1,473 / 1,473 | 1,228 | 245 | 21.209 | 30.548 | 39.028 | 49.09 |
| 4 | 30.021 | 5,889 / 5,889 | 4,908 | 981 | 21.585 | 29.840 | 38.189 | 196.16 |
| 8 | 30.026 | 7,030 / 7,030 | 5,859 | 1,171 | 34.923 | 54.832 | 67.533 | 234.13 |

持续测试合计 **14,392次**，其中 **11,995次正常200**、**2,397次预期且正确的403**。授权误放、误拒、数据oracle错误和其他异常均为0。403来自普通员工尝试读取采购成本字段，属于成功验证权限边界，不记为服务错误；表内延迟是包含此类拒绝请求的混合工作负载统计。

测试负载是受授权的本人月工资点查、部门近期考勤最多100行窗口、指定仓库/SKU库存点查、采购价授权读取与拒绝、跨租户同SKU隔离。它证明的是大库上的**索引点查和有限结果读取**，没有覆盖任意聚合、任意跨表连接、全表导出、无限分页或混合写入事务。API最多返回100行，不能据此声称Agent已经扫描整库或完成全量统计。90秒分档持续读、热缓存、本地网络的结果，也不能外推为长时间稳态、冷缓存或生产集群容量。

持续证据为 `.local/scale/benchmark-sustained.json` 和 `.local/scale-benchmark-sustained.log`；之前的直接SQL、突发API与完整EXPLAIN证据保留不变。可复核的只读完整性SQL另存 `fixtures/scale/hr_verify.sql`、`fixtures/scale/hardware_verify.sql`。
