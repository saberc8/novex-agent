-- Independent product/WMS database; all values synthetic.
CREATE TABLE IF NOT EXISTS public.novex_demo_products (
 tenant_id text NOT NULL, sku text NOT NULL, name text NOT NULL, category text NOT NULL,
 spec text NOT NULL, sale_price numeric(12,2) NOT NULL, cost_price numeric(12,2) NOT NULL,
 supplier text NOT NULL, PRIMARY KEY(tenant_id,sku)
);
CREATE TABLE IF NOT EXISTS public.novex_demo_inventory (
 tenant_id text NOT NULL, warehouse_id text NOT NULL, warehouse_name text NOT NULL,
 sku text NOT NULL, product_name text NOT NULL, stock integer NOT NULL,
 reserved integer NOT NULL, reorder_level integer NOT NULL,
 PRIMARY KEY(tenant_id,warehouse_id,sku)
);
INSERT INTO public.novex_demo_products VALUES
 ('acme','HW-001','不锈钢六角螺栓 M8×30','紧固件','304不锈钢 / M8×30 / 100枚装',68,38,'演示供应商甲'),
 ('acme','HW-002','工业轴承 6204-2RS','传动件','20×47×14mm / 双面密封',29,16,'演示供应商乙'),
 ('acme','HW-003','电动扭矩扳手 18V','电动工具','18V / 20–200Nm / 双电池',899,520,'演示供应商丙'),
 ('globex','HW-001','另一租户专用紧固件','紧固件','不可跨租户读取',199,99,'私有供应商')
 ON CONFLICT DO NOTHING;
INSERT INTO public.novex_demo_inventory VALUES
 ('acme','W1','上海一号仓','HW-001','不锈钢六角螺栓 M8×30',800,120,200),
 ('acme','W1','上海一号仓','HW-002','工业轴承 6204-2RS',42,10,50),
 ('acme','W1','上海一号仓','HW-003','电动扭矩扳手 18V',25,3,10),
 ('acme','W2','深圳二号仓','HW-001','不锈钢六角螺栓 M8×30',320,20,100),
 ('acme','W2','深圳二号仓','HW-002','工业轴承 6204-2RS',180,30,50),
 ('globex','W1','另一租户私有仓','HW-001','另一租户专用紧固件',9999,0,100)
 ON CONFLICT DO NOTHING;

-- Incremental WMS catalog and movement ledger. Positive quantity + movement_type
-- identifies direction. Synthetic ledger totals reconcile with the inventory above.
CREATE TABLE IF NOT EXISTS public.novex_demo_warehouses (
 tenant_id text NOT NULL, warehouse_id text NOT NULL, warehouse_name text NOT NULL,
 address text NOT NULL, manager_employee_id text NOT NULL,
 PRIMARY KEY(tenant_id,warehouse_id)
);
CREATE TABLE IF NOT EXISTS public.novex_demo_stock_movements (
 tenant_id text NOT NULL, movement_id text NOT NULL, warehouse_id text NOT NULL,
 sku text NOT NULL, movement_type text NOT NULL CHECK(movement_type IN ('inbound','outbound')),
 quantity integer NOT NULL CHECK(quantity>0), occurred_at timestamptz NOT NULL,
 document_no text NOT NULL, operator_employee_id text NOT NULL,
 PRIMARY KEY(tenant_id,movement_id)
);
INSERT INTO public.novex_demo_warehouses VALUES
 ('acme','W1','上海一号仓','上海市演示工业园A区（合成地址）','E002'),
 ('acme','W2','深圳二号仓','深圳市演示物流园B区（合成地址）','E005'),
 ('globex','W1','另一租户私有仓','Globex演示物流园（合成地址）','E001')
 ON CONFLICT DO NOTHING;
INSERT INTO public.novex_demo_stock_movements VALUES
 ('acme','MOV-SH-001-IN','W1','HW-001','inbound',1000,'2026-11-02 09:00:00+08','IN-SH-20261102-001','E002'),
 ('acme','MOV-SH-001-OUT','W1','HW-001','outbound',200,'2026-11-05 14:00:00+08','OUT-SH-20261105-001','E002'),
 ('acme','MOV-SH-002-IN','W1','HW-002','inbound',60,'2026-11-03 10:00:00+08','IN-SH-20261103-002','E002'),
 ('acme','MOV-SH-002-OUT','W1','HW-002','outbound',18,'2026-11-05 15:30:00+08','OUT-SH-20261105-002','E002'),
 ('acme','MOV-SH-003-IN','W1','HW-003','inbound',30,'2026-11-03 11:00:00+08','IN-SH-20261103-003','E002'),
 ('acme','MOV-SH-003-OUT','W1','HW-003','outbound',5,'2026-11-05 16:00:00+08','OUT-SH-20261105-003','E002'),
 ('acme','MOV-SZ-001-IN','W2','HW-001','inbound',400,'2026-11-02 09:30:00+08','IN-SZ-20261102-001','E005'),
 ('acme','MOV-SZ-001-OUT','W2','HW-001','outbound',80,'2026-11-05 13:00:00+08','OUT-SZ-20261105-001','E005'),
 ('acme','MOV-SZ-002-IN','W2','HW-002','inbound',200,'2026-11-03 10:30:00+08','IN-SZ-20261103-002','E005'),
 ('acme','MOV-SZ-002-OUT','W2','HW-002','outbound',20,'2026-11-05 16:30:00+08','OUT-SZ-20261105-002','E005'),
 ('globex','MOV-GX-001-IN','W1','HW-001','inbound',10000,'2026-11-02 09:00:00+08','IN-GX-20261102-001','E001'),
 ('globex','MOV-GX-001-OUT','W1','HW-001','outbound',1,'2026-11-05 17:00:00+08','OUT-GX-20261105-001','E001')
 ON CONFLICT DO NOTHING;
