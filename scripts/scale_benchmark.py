#!/usr/bin/env python3
"""Bounded read-only SQL and real permission-enforced API load tests.
No Agent/model calls; latency measured client-side against warmed local services.
"""
import argparse, concurrent.futures, json, math, os, shlex, threading, time, urllib.request, urllib.error
from pathlib import Path
import psycopg
ROOT=Path(__file__).resolve().parents[1];OUT=ROOT/'.local/scale'

def env_values():
 values={}
 for line in (ROOT/'.env.local').read_text().splitlines():
  if '=' in line and not line.startswith('#'):
   key,value=line.split('=',1)
   try:values[key]=shlex.split(value)[0] if value else ''
   except ValueError:pass
 return values
ENV=env_values()
SQL_CASES=[
 ('hr_self_payroll','hr',"SELECT employee_id,total,net_pay FROM payroll WHERE tenant_id=%s AND employee_id=%s AND month=%s",('SC001','E001','2026-10')),
 ('hr_department_attendance','hr',"SELECT employee_id,work_date,status,work_minutes FROM attendance WHERE tenant_id=%s AND department_id=%s AND work_date>=%s ORDER BY work_date DESC LIMIT 100",('SC001','D001','2026-10-01')),
 ('hr_employee_leave','hr',"SELECT leave_id,start_date,end_date,status FROM leave_requests WHERE tenant_id=%s AND employee_id=%s ORDER BY start_date DESC LIMIT 20",('SC001','E010')),
 ('wms_inventory_anchor','hardware',"SELECT sku,stock,reserved FROM inventory WHERE tenant_id=%s AND warehouse_id=%s AND sku=%s",('SC001','W1','HW-000002')),
 ('wms_latest_movements','hardware',"SELECT sku,movement_type,quantity,occurred_at FROM stock_movements WHERE tenant_id=%s AND warehouse_id=%s AND sku=%s ORDER BY occurred_at DESC LIMIT 20",('SC001','W1','HW-000002')),
 ('wms_orders_page','hardware',"SELECT order_id,status,created_at FROM orders WHERE tenant_id=%s AND warehouse_id=%s ORDER BY created_at DESC LIMIT 100",('SC001','W1')),
]
API_CASES=[
 ('api_self_payroll','scale_a',{'resourceId':'scale_hr_payroll','filters':[{'field':'month','op':'eq','value':'2026-10'}],'limit':20},200),
 ('api_department_attendance','scale_b',{'resourceId':'scale_hr_attendance','filters':[{'field':'work_date','op':'gte','value':'2026-10-01'}],'sort':{'field':'work_date','direction':'desc'},'limit':100},200),
 ('api_warehouse_inventory','scale_b',{'resourceId':'scale_wms_inventory','filters':[{'field':'sku','op':'eq','value':'HW-000002'}],'limit':20},200),
 ('api_procurement_cost','scale_c',{'resourceId':'scale_wms_products','fields':['sku','cost_price'],'filters':[{'field':'sku','op':'eq','value':'HW-000002'}],'limit':20},200),
 ('api_cost_denied','scale_a',{'resourceId':'scale_wms_products','fields':['sku','cost_price'],'filters':[{'field':'sku','op':'eq','value':'HW-000002'}]},403),
 ('api_cross_tenant_anchor','scale_x',{'resourceId':'scale_wms_inventory','filters':[{'field':'sku','op':'eq','value':'HW-000001'}]},200),
]

def summary(samples,seconds,errors,workers):
 values=sorted(samples);at=lambda q:round(values[min(len(values)-1,math.ceil(q*len(values))-1)],3) if values else None
 return {'requests':len(samples)+len(errors),'successful':len(samples),'expected_status_errors':len(errors),'concurrency':workers,'wall_seconds':round(seconds,3),'throughput_rps':round((len(samples)+len(errors))/seconds,2),'latency_ms':{'p50':at(.5),'p95':at(.95),'p99':at(.99),'max':round(max(values),3) if values else None},'errors':errors[:3]}

def sql_bench(workers,count):
 cache=threading.local();connections=[];lock=threading.Lock();errors=[];samples=[]
 def connection(kind):
  if not hasattr(cache,'db'):cache.db={}
  if kind not in cache.db:
   prefix='NOVEX_'+kind.upper()+'_SCALE_';c=psycopg.connect(host=ENV[prefix+'HOST'],port=ENV[prefix+'PORT'],user=ENV[prefix+'USER'],password=ENV[prefix+'PASSWORD'],dbname=ENV[prefix+'DATABASE'],autocommit=True,application_name='novex-scale-sql-benchmark');cache.db[kind]=c
   with lock:connections.append(c)
  return cache.db[kind]
 def run(case):
  name,kind,query,params=case;start=time.perf_counter()
  try:
   rows=connection(kind).execute(query,params).fetchall()
   if not rows:raise RuntimeError('unexpected empty result')
   return (time.perf_counter()-start)*1000,None
  except Exception as error:return None,{'case':name,'error':type(error).__name__}
 before=time.perf_counter()
 with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as pool:
  for latency,error in pool.map(run,[SQL_CASES[n%len(SQL_CASES)] for n in range(count)]):
   if error:errors.append(error)
   else:samples.append(latency)
 elapsed=time.perf_counter()-before
 for c in connections:c.close()
 return summary(samples,elapsed,errors,workers)

def request(base,path,body,token=''):
 data=json.dumps(body).encode();req=urllib.request.Request(base+'/api/novex'+path,data=data,headers={'Content-Type':'application/json',**({'Authorization':'Bearer '+token} if token else {})})
 try:
  with urllib.request.urlopen(req,timeout=15) as response:return response.status,json.load(response)
 except urllib.error.HTTPError as error:return error.code,json.load(error)

def api_bench(base,tokens,workers,count,duration=0):
 permission_counts={'expected_denials':0,'correct_denials':0,'unexpected_allows':0,'unexpected_denials':0,'expected_allowed_requests':0};permission_lock=threading.Lock()
 def run(case):
  name,username,body,status=case;before=time.perf_counter()
  try:
   actual,response=request(base,'/query',body,tokens[username])
   with permission_lock:
    permission_counts['expected_denials' if status==403 else 'expected_allowed_requests']+=1
    if status==403 and actual==403:permission_counts['correct_denials']+=1
    if status==403 and actual==200:permission_counts['unexpected_allows']+=1
    if status==200 and actual==403:permission_counts['unexpected_denials']+=1
   if actual!=status:return None,{'case':name,'expected':status,'actual':actual,'message':response.get('msg')}
   if status==200:
    rows=response['data']['rows']
    if not rows:return None,{'case':name,'error':'empty result'}
    if name=='api_self_payroll' and (len(rows)!=1 or str(rows[0].get('total'))!='9000.00'):return None,{'case':name,'error':'salary oracle mismatch'}
    if name=='api_warehouse_inventory' and any(r.get('warehouse_id')!='W1' or r.get('stock')!=42 or r.get('reserved')!=10 for r in rows):return None,{'case':name,'error':'warehouse oracle mismatch'}
    if name=='api_cross_tenant_anchor' and any(r.get('stock')!=9999 for r in rows):return None,{'case':name,'error':'tenant oracle mismatch'}
   return (time.perf_counter()-before)*1000,None
  except Exception as error:return None,{'case':name,'error':type(error).__name__}
 before=time.perf_counter();samples=[];errors=[]
 with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as pool:
  if duration:
   deadline=before+duration;counter=[0];counter_lock=threading.Lock()
   def sustained_worker(_):
    local=[]
    while time.perf_counter()<deadline:
     with counter_lock:index=counter[0];counter[0]+=1
     local.append(run(API_CASES[index%len(API_CASES)]))
    return local
   outcomes=[]
   for local in pool.map(sustained_worker,range(workers)):outcomes.extend(local)
  else:outcomes=pool.map(run,[API_CASES[n%len(API_CASES)] for n in range(count)])
  for latency,error in outcomes:
   if error:errors.append(error)
   else:samples.append(latency)
 result=summary(samples,time.perf_counter()-before,errors,workers)
 result['target_duration_seconds']=duration or None
 permission_counts['false_allow_rate']=permission_counts['unexpected_allows']/max(1,permission_counts['expected_denials'])
 permission_counts['false_deny_rate']=permission_counts['unexpected_denials']/max(1,permission_counts['expected_allowed_requests'])
 result['authorization']=permission_counts
 return result

def explain_queries():
 plans=[]
 for name,kind,query,params in SQL_CASES:
  prefix='NOVEX_'+kind.upper()+'_SCALE_'
  with psycopg.connect(host=ENV[prefix+'HOST'],port=ENV[prefix+'PORT'],user=ENV[prefix+'USER'],password=ENV[prefix+'PASSWORD'],dbname=ENV[prefix+'DATABASE'],autocommit=True,application_name='novex-scale-explain') as c:
   plan=c.execute('EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) '+query,params).fetchone()[0][0]
  def indexes(node):
   found=[node['Index Name']] if 'Index Name' in node else []
   for child in node.get('Plans',[]):found+=indexes(child)
   return found
  plans.append({'case':name,'database':kind,'query':query,'parameters':params,'planning_ms':plan['Planning Time'],'execution_ms':plan['Execution Time'],'indexes':indexes(plan['Plan']),'plan':plan})
 OUT.mkdir(exist_ok=True,parents=True);(OUT/'explain-analyze.json').write_text(json.dumps(plans,indent=2,ensure_ascii=False))
 return [{k:p[k] for k in ['case','database','planning_ms','execution_ms','indexes']} for p in plans]

def main():
 p=argparse.ArgumentParser();p.add_argument('--requests',type=int,default=180);p.add_argument('--api-url',default='http://127.0.0.1:8310');p.add_argument('--skip-api',action='store_true');p.add_argument('--api-only',action='store_true');p.add_argument('--duration',type=int,default=0,help='API sustained seconds per concurrency level; 0 uses fixed request count');args=p.parse_args()
 if args.requests<12 or args.requests>3000:raise SystemExit('request count must be 12..3000 per concurrency level')
 if args.duration<0 or args.duration>300:raise SystemExit('duration must be 0..300 seconds per level')
 if args.api_only and args.skip_api:raise SystemExit('--api-only and --skip-api conflict')
 results={'note':'Local warmed-cache read benchmark; client latency includes connection/setup for each worker. Expected authorization denials count as successful checks. Not model latency or production capacity proof.','sql_cases':[c[0] for c in SQL_CASES],'api_cases':[c[0] for c in API_CASES],'sql':[],'api':[],'explain':[] if args.api_only else explain_queries(),'target_duration_seconds':args.duration}
 if not args.api_only:
  for workers in [1,4,8]:
   result=sql_bench(workers,args.requests);results['sql'].append(result);print(json.dumps({'layer':'sql',**result}),flush=True)
 if not args.skip_api:
  tokens={}
  for username in sorted({c[1] for c in API_CASES}):
   status,result=request(args.api_url,'/login',{'username':username,'password':ENV['BOOTSTRAP_ADMIN_PASSWORD']})
   if status!=200:raise RuntimeError('Scale account login failed: '+username)
   tokens[username]=result['data']['token']
  for workers in [1,4,8]:
   result=api_bench(args.api_url,tokens,workers,args.requests,args.duration);results['api'].append(result);print(json.dumps({'layer':'api',**result}),flush=True)
 OUT.mkdir(exist_ok=True,parents=True);(OUT/('benchmark-sustained.json' if args.duration else 'benchmark.json')).write_text(json.dumps(results,indent=2,ensure_ascii=False))
 if any(r['expected_status_errors'] for layer in ['sql','api'] for r in results[layer]):raise SystemExit('Benchmark had unexpected errors; see report')
if __name__=='__main__':main()
