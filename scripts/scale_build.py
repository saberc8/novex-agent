#!/usr/bin/env python3
"""Build/resume two isolated relational scale databases; never touches demo databases.
Requires psycopg 3. Credentials are read privately from .local/bootstrap.json.
One loading connection at a time, transactional batches, actual pg_database_size gate.
"""
import argparse, json, os, re, secrets, shlex, shutil, time
from pathlib import Path
from datetime import date
import psycopg
from psycopg import sql
ROOT=Path(__file__).resolve().parents[1]
FIX=ROOT/'fixtures/scale'
OUT=ROOT/'.local/scale'
TARGET=2*1024**3
TABLES={
 'hr':['companies','departments','employees','contracts','payroll','attendance','leave_requests'],
 'hardware':['companies','products','warehouses','locations','inventory','orders','order_items','stock_movements','after_sales']}

def event(**data): print(json.dumps(data,ensure_ascii=False),flush=True)
def env_values():
 out={}
 for line in (ROOT/'.env.local').read_text().splitlines():
  if not line.strip() or line.lstrip().startswith('#') or '=' not in line: continue
  k,v=line.split('=',1)
  try: out[k]=shlex.split(v)[0] if v else ''
  except ValueError: continue
 return out

def update_env(values):
 path=ROOT/'.env.local';lines=path.read_text().splitlines();seen=set();out=[]
 for line in lines:
  key=line.split('=',1)[0] if '=' in line else ''
  if key in values:
   if key not in seen: out.append(key+'='+shlex.quote(str(values[key])));seen.add(key)
  else: out.append(line)
 for key,value in values.items():
  if key not in seen:out.append(key+'='+shlex.quote(str(value)))
 temporary=path.with_name('.env.local.scale.tmp');temporary.write_text('\n'.join(out)+'\n');temporary.chmod(0o600);os.replace(temporary,path)

def state(c,key,default=None):
 row=c.execute('SELECT value FROM scale_meta WHERE key=%s',(key,)).fetchone()
 return row[0] if row else default

def save(c,key,value):c.execute('INSERT INTO scale_meta(key,value) VALUES(%s,%s::jsonb) ON CONFLICT(key) DO UPDATE SET value=excluded.value',(key,json.dumps(value)))
def size(c):return c.execute('SELECT pg_database_size(current_database())').fetchone()[0]
def guard_disk():
 if shutil.disk_usage(ROOT).free<8*1024**3:raise RuntimeError('Less than 8 GiB disk free; loader paused without deleting anything')

def provision(kind,cfg):
 dbname='novex-'+kind+'-scale';role='novex_'+kind+'_scale_read';prefix='NOVEX_'+kind.upper()+'_SCALE_';current=env_values();password=current.get(prefix+'PASSWORD') or secrets.token_urlsafe(32)
 with psycopg.connect(**cfg,autocommit=True) as admin:
  if not admin.execute('SELECT 1 FROM pg_database WHERE datname=%s',(dbname,)).fetchone():admin.execute(sql.SQL('CREATE DATABASE {}').format(sql.Identifier(dbname)))
  if not admin.execute('SELECT 1 FROM pg_roles WHERE rolname=%s',(role,)).fetchone():admin.execute(sql.SQL('CREATE ROLE {} LOGIN PASSWORD {} NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS').format(sql.Identifier(role),sql.Literal(password)))
  else:admin.execute(sql.SQL('ALTER ROLE {} PASSWORD {} NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS').format(sql.Identifier(role),sql.Literal(password)))
  admin.execute(sql.SQL('ALTER ROLE {} SET default_transaction_read_only=on').format(sql.Identifier(role)))
  admin.execute(sql.SQL("ALTER ROLE {} SET statement_timeout='8s'").format(sql.Identifier(role)))
  admin.execute(sql.SQL('REVOKE ALL ON DATABASE {} FROM PUBLIC').format(sql.Identifier(dbname)))
  admin.execute(sql.SQL('GRANT CONNECT ON DATABASE {} TO {}').format(sql.Identifier(dbname),sql.Identifier(role)))
 update_env({prefix+'HOST':cfg['host'],prefix+'PORT':cfg['port'],prefix+'DATABASE':dbname,prefix+'USER':role,prefix+'PASSWORD':password})
 return dbname,role

HR_ATTENDANCE="""
INSERT INTO attendance
SELECT e.tenant_id,e.employee_id,e.department_id,d.work_date,'DAY-0900',
CASE WHEN st.status IN('rest','leave') THEN NULL ELSE d.work_date+time '09:00'+CASE WHEN st.status='late' THEN interval '12 minutes' ELSE interval '0 minutes' END END,
CASE WHEN st.status IN('rest','leave') THEN NULL ELSE d.work_date+time '18:00'+CASE WHEN e.seed_no%17=0 THEN interval '1 hour' ELSE interval '0 hours' END END,
CASE WHEN st.status IN('rest','leave') THEN 0 WHEN st.status='late' THEN 468 ELSE 480 END,
CASE WHEN st.status NOT IN('rest','leave') AND e.seed_no%17=0 THEN 60 ELSE 0 END,
st.status,'业务园区-'||e.department_id,CASE WHEN st.status='leave' THEN '关联已批准年度休假LEAVE-'||e.employee_id WHEN st.status='rest' THEN '周末休息，无出勤要求' WHEN st.status='late' THEN '系统记录迟到12分钟，待部门确认' ELSE '门禁与排班记录核对一致，标准工时含一小时午休' END
FROM employees e CROSS JOIN (SELECT date '2026-11-05'-n AS work_date FROM generate_series({start},{end})n)d
CROSS JOIN LATERAL(SELECT CASE WHEN extract(isodow FROM d.work_date)>=6 THEN 'rest' WHEN d.work_date BETWEEN date '2026-10-19' AND date '2026-10-20' AND e.seed_no%10=0 THEN 'leave' WHEN e.seed_no%113=0 AND extract(isodow FROM d.work_date) IN(2,4) THEN 'late' ELSE 'present' END AS status)st
"""

HARDWARE_BATCH="""
CREATE TEMP TABLE batch_lines ON COMMIT DROP AS
SELECT w.tenant_id,w.warehouse_id,p.sku,p.sku_no,'L'||lpad(((p.sku_no-1)%100+1)::text,3,'0') AS location_id,
 ((p.sku_no-4)/5+1) AS group_no,((p.sku_no-4)%5+1) AS line_no,
 'PO-B{batch}-'||w.warehouse_id||'-G'||((p.sku_no-4)/5+1) AS purchase_id,
 'SO-B{batch}-'||w.warehouse_id||'-G'||((p.sku_no-4)/5+1) AS sales_id,
 100+p.sku_no%20 AS in_qty,60+p.sku_no%10 AS out_qty,p.cost_price,p.sale_price,w.manager_employee_id
FROM warehouses w JOIN products p USING(tenant_id) WHERE p.sku_no>=4;
INSERT INTO orders
SELECT tenant_id,purchase_id,warehouse_id,'purchase','received',timestamptz '2026-11-05 09:00:00+08'-interval '{batch} days','合成供应商批次B{batch}',manager_employee_id,sum(in_qty*cost_price),'仓库收货月台'||warehouse_id,'采购补货单，逐行验收后入库；每单最多5个SKU，合成测试业务。' FROM batch_lines GROUP BY tenant_id,purchase_id,warehouse_id,manager_employee_id;
INSERT INTO orders
SELECT tenant_id,sales_id,warehouse_id,'sales','shipped',timestamptz '2026-11-05 15:00:00+08'-interval '{batch} days','合成工业客户批次B{batch}',manager_employee_id,sum(out_qty*sale_price),'客户工业园区配送点'||warehouse_id,'销售发货单，已复核出库；数量与出库流水逐行关联。' FROM batch_lines GROUP BY tenant_id,sales_id,warehouse_id,manager_employee_id;
INSERT INTO order_items SELECT tenant_id,purchase_id,line_no,warehouse_id,sku,in_qty,cost_price,in_qty*cost_price FROM batch_lines;
INSERT INTO order_items SELECT tenant_id,sales_id,line_no,warehouse_id,sku,out_qty,sale_price,out_qty*sale_price FROM batch_lines;
INSERT INTO stock_movements SELECT tenant_id,'MOV-'||purchase_id||'-'||line_no,warehouse_id,location_id,sku,'inbound',in_qty,timestamptz '2026-11-05 09:30:00+08'-interval '{batch} days',purchase_id,purchase_id,line_no,manager_employee_id,'采购收货，质检通过后按指定库位上架' FROM batch_lines;
INSERT INTO stock_movements SELECT tenant_id,'MOV-'||sales_id||'-'||line_no,warehouse_id,location_id,sku,'outbound',out_qty,timestamptz '2026-11-05 15:30:00+08'-interval '{batch} days',sales_id,sales_id,line_no,manager_employee_id,'销售出库，扫描复核并生成运输交接单' FROM batch_lines;
UPDATE inventory i SET stock=i.stock+b.in_qty-b.out_qty FROM batch_lines b WHERE i.tenant_id=b.tenant_id AND i.warehouse_id=b.warehouse_id AND i.sku=b.sku;
INSERT INTO after_sales SELECT tenant_id,'AS-B{batch}-'||warehouse_id||'-'||sku,warehouse_id,sales_id,line_no,sku,'合成工业客户批次B{batch}',timestamptz '2026-11-05 17:30:00+08'-interval '{batch} days','resolved','installation','客户咨询安装方向，远程指导后确认正常，无退货及库存变动。',(date '2026-11-05'-{batch}+interval '12 months')::date,'已发送安装说明并完成客户确认。' FROM batch_lines WHERE sku_no%50=0;
"""

def finalize(c,kind,role):
 # Add and validate FKs after bulk loading instead of paying per-row trigger overhead.
 for statement in (FIX/(kind+'_constraints.sql')).read_text().split(';'):
  statement=statement.strip()
  if not statement:continue
  match=re.search(r'ALTER TABLE (\w+) ADD CONSTRAINT (\w+)',statement);table,name=match.groups()
  exists=c.execute('SELECT convalidated FROM pg_constraint WHERE conname=%s AND conrelid=%s::regclass',(name,table)).fetchone()
  if not exists:c.execute(statement);c.commit()
  if not exists or not exists[0]:
   before=time.monotonic();c.execute(sql.SQL('ALTER TABLE {} VALIDATE CONSTRAINT {}').format(sql.Identifier(table),sql.Identifier(name)));c.commit();event(stage='validate_fk',database=kind,constraint=name,seconds=round(time.monotonic()-before,2))
 c.execute(sql.SQL('GRANT USAGE ON SCHEMA public TO {}').format(sql.Identifier(role)))
 c.execute(sql.SQL('GRANT SELECT ON ALL TABLES IN SCHEMA public TO {}').format(sql.Identifier(role)))
 c.execute(sql.SQL('ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO {}').format(sql.Identifier(role)))
 c.commit();c.execute('ANALYZE');c.commit()

HR_CHECKS={
 'employee_department_mismatch':"SELECT count(*) FROM employees e LEFT JOIN departments d USING(tenant_id,department_id) WHERE d.department_id IS NULL",
 'payroll_employee_or_department_mismatch':"SELECT count(*) FROM payroll p JOIN employees e USING(tenant_id,employee_id) WHERE p.department_id<>e.department_id",
 'payroll_calculation_mismatch':"SELECT count(*) FROM payroll WHERE total<>base+bonus+overtime_pay OR net_pay<>total-social_insurance-tax",
 'attendance_employee_department_mismatch':"SELECT count(*) FROM attendance a JOIN employees e USING(tenant_id,employee_id) WHERE a.department_id<>e.department_id",
 'leave_attendance_mismatch':"SELECT count(*) FROM leave_requests l JOIN attendance a USING(tenant_id,employee_id) WHERE a.work_date BETWEEN l.start_date AND l.end_date AND l.status='approved' AND a.status<>'leave'",
}
WMS_CHECKS={
 'order_line_amount_mismatch':"SELECT count(*) FROM order_items WHERE line_total<>quantity*unit_price",
 'order_total_mismatch':"SELECT count(*) FROM orders o JOIN (SELECT tenant_id,order_id,sum(line_total) amount FROM order_items GROUP BY tenant_id,order_id)i USING(tenant_id,order_id) WHERE o.total_amount<>i.amount",
 'movement_order_line_mismatch':"SELECT count(*) FROM stock_movements m JOIN order_items i USING(tenant_id,order_id,line_no) WHERE m.sku<>i.sku OR m.warehouse_id<>i.warehouse_id OR m.quantity<>i.quantity",
 'inventory_ledger_mismatch':"SELECT count(*) FROM inventory i FULL JOIN (SELECT tenant_id,warehouse_id,sku,sum(CASE WHEN movement_type='inbound' THEN quantity ELSE -quantity END) balance FROM stock_movements GROUP BY tenant_id,warehouse_id,sku)m USING(tenant_id,warehouse_id,sku) WHERE i.stock IS DISTINCT FROM COALESCE(m.balance,0)",
 'reserved_order_mismatch':"SELECT count(*) FROM inventory i LEFT JOIN (SELECT oi.tenant_id,oi.warehouse_id,oi.sku,sum(oi.quantity) qty FROM order_items oi JOIN orders o USING(tenant_id,order_id) WHERE o.status='allocated' GROUP BY oi.tenant_id,oi.warehouse_id,oi.sku)r USING(tenant_id,warehouse_id,sku) WHERE i.reserved<>COALESCE(r.qty,0)",
 'invalid_inventory':"SELECT count(*) FROM inventory WHERE stock<0 OR reserved<0 OR reserved>stock",
}

def verify(c,kind):
 checks={}
 for name,query in (HR_CHECKS if kind=='hr' else WMS_CHECKS).items():
  before=time.monotonic();count=c.execute(query).fetchone()[0];checks[name]={'violations':count,'seconds':round(time.monotonic()-before,3)};event(stage='integrity',database=kind,check=name,violations=count,seconds=checks[name]['seconds'])
  if count:raise RuntimeError('Integrity check failed: '+name)
 invalid=c.execute("SELECT count(*) FROM pg_constraint WHERE contype='f' AND NOT convalidated").fetchone()[0]
 checks['unvalidated_foreign_keys']={'violations':invalid}
 if invalid:raise RuntimeError('Unvalidated foreign keys remain')
 return checks

def build(kind,cfg,target):
 started=time.monotonic();dbname,role=provision(kind,cfg);conn_cfg={**cfg,'dbname':dbname,'application_name':'novex-scale-builder'}
 with psycopg.connect(**conn_cfg) as c:
  c.execute("SET synchronous_commit=off; SET max_parallel_workers_per_gather=0; SET work_mem='32MB'; SET maintenance_work_mem='256MB'; SET statement_timeout=0")
  c.execute((FIX/(kind+'_schema.sql')).read_text());c.commit()
  if not state(c,'base_complete',False):
   guard_disk();before=time.monotonic();c.execute((FIX/(kind+'_base.sql')).read_text());save(c,'base_complete',True);save(c,'started_at',time.time());c.commit();event(stage='base_complete',database=dbname,size_bytes=size(c),seconds=round(time.monotonic()-before,2))
  batches=state(c,'batches',0)
  while size(c)<target:
   guard_disk();before=time.monotonic()
   if kind=='hr':c.execute(HR_ATTENDANCE.format(start=batches*7,end=batches*7+6))
   else:c.execute(HARDWARE_BATCH.format(batch=batches+1))
   batches+=1;save(c,'batches',batches);c.commit();event(stage='batch',database=dbname,batch=batches,size_bytes=size(c),seconds=round(time.monotonic()-before,2))
   if batches>120:raise RuntimeError('Batch safety cap reached')
  finalize(c,kind,role);checks=verify(c,kind)
  counts={table:c.execute(sql.SQL('SELECT count(*) FROM {}').format(sql.Identifier(table))).fetchone()[0] for table in TABLES[kind]}
  relations=c.execute("SELECT relname,pg_relation_size(oid),pg_indexes_size(oid),pg_total_relation_size(oid) FROM pg_class WHERE relkind='r' AND relnamespace='public'::regnamespace ORDER BY pg_total_relation_size(oid) DESC").fetchall()
  report={'database':dbname,'host':cfg['host'],'port':cfg['port'],'read_only_role':role,'size_bytes':size(c),'minimum_bytes':target,'batches':batches,'rows':counts,'total_rows':sum(counts.values()),'integrity':checks,'relations':[{'table':x[0],'heap_bytes':x[1],'index_bytes':x[2],'total_bytes':x[3]} for x in relations],'current_run_seconds':round(time.monotonic()-started,3),'generation_started_at':state(c,'started_at'),'completed_at':time.time()}
  report['elapsed_seconds']=round(report['completed_at']-report['generation_started_at'],3)
  if report['size_bytes']<target:raise RuntimeError('Actual database size below target')
  save(c,'complete_report',report);c.commit();OUT.mkdir(parents=True,exist_ok=True);(OUT/(kind+'-build.json')).write_text(json.dumps(report,indent=2,ensure_ascii=False));event(stage='complete',**{k:report[k] for k in ['database','size_bytes','total_rows','current_run_seconds']});return report

def main():
 parser=argparse.ArgumentParser();parser.add_argument('--database',choices=['hr','hardware','both'],default='both');parser.add_argument('--target-gib',type=float,default=2.0);args=parser.parse_args();target=int(args.target_gib*1024**3)
 if target<TARGET:raise SystemExit('Target must be at least 2 GiB')
 cfg=json.loads((ROOT/'.local/bootstrap.json').read_text());reports=[]
 for kind in (['hr','hardware'] if args.database=='both' else [args.database]):reports.append(build(kind,cfg,target))
 (OUT/'build-manifest.json').write_text(json.dumps(reports,indent=2,ensure_ascii=False))
if __name__=='__main__':main()
