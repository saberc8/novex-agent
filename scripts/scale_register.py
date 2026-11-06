#!/usr/bin/env python3
"""Register isolated scale sources/resources/accounts using the real Novex APIs.
Run only before regression conversations: metadata changes revoke old run contexts.
Does not modify the original a/b/c/x users, demo sources or demo grants.
"""
import json, os, shlex, urllib.request, urllib.error
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]

def read_env():
 values={}
 for line in (ROOT/'.env.local').read_text().splitlines():
  if '=' in line and not line.lstrip().startswith('#'):
   k,v=line.split('=',1)
   try:values[k]=shlex.split(v)[0] if v else ''
   except ValueError:pass
 return values
ENV=read_env();BASE=os.environ.get('NOVEX_API_URL','http://127.0.0.1:8310')+'/api/novex';TOKEN=''
def call(method,path,data=None):
 body=json.dumps(data).encode() if data is not None else None
 request=urllib.request.Request(BASE+path,data=body,method=method,headers={'Content-Type':'application/json',**({'Authorization':'Bearer '+TOKEN} if TOKEN else {})})
 try:
  with urllib.request.urlopen(request,timeout=30) as response:result=json.load(response)
 except urllib.error.HTTPError as error:
  try:message=json.load(error).get('msg','request failed')
  except Exception:message='request failed'
  raise RuntimeError(f'{method} {path}: HTTP {error.code}: {message}') from None
 if not result.get('success'):raise RuntimeError(result.get('msg','API failed'))
 return result['data']

def fields(*names):return {name:'plain' for name in names}
RESOURCE_SPECS=[
 ('hr','companies','规模·企业','', '', '', ['tenant_id','company_name','industry','registered_city','created_at']),
 ('hr','departments','规模·部门','', 'department_id','', ['department_id','name','parent_department_id','manager_employee_id','cost_center']),
 ('hr','employees','规模·员工目录','employee_id','department_id','', ['employee_id','employee_name','department_id','job_title','employment_status','hire_date','email','phone','bank_account','address']),
 ('hr','contracts','规模·劳动合同','employee_id','department_id','', ['contract_id','employee_id','department_id','start_date','end_date','contract_type','base_salary','work_location','terms_summary']),
 ('hr','payroll','规模·工资单','employee_id','department_id','', ['employee_id','employee_name','department_id','month','base','bonus','overtime_pay','total','social_insurance','tax','net_pay','bank_account','payment_status','payment_date','payroll_note']),
 ('hr','attendance','规模·考勤','employee_id','department_id','', ['employee_id','department_id','work_date','shift_code','clock_in','clock_out','work_minutes','overtime_minutes','status','work_site','note']),
 ('hr','leave_requests','规模·请假','employee_id','department_id','', ['leave_id','employee_id','department_id','start_date','end_date','days','leave_type','status','approver_employee_id','reason']),
 ('hardware','products','规模·五金产品','','','', ['sku','name','category','spec','unit','sale_price','cost_price','supplier','warranty_months','description']),
 ('hardware','warehouses','规模·仓库','','','warehouse_id',['warehouse_id','warehouse_name','address','manager_employee_id']),
 ('hardware','locations','规模·库位','','','warehouse_id',['warehouse_id','location_id','zone','aisle','rack','capacity_units']),
 ('hardware','inventory','规模·实时库存','','','warehouse_id',['warehouse_id','warehouse_name','location_id','sku','product_name','stock','reserved','reorder_level','last_counted_at']),
 ('hardware','orders','规模·采购销售订单','employee_id','','warehouse_id',['order_id','warehouse_id','order_type','status','created_at','counterparty','employee_id','total_amount','delivery_address','order_note']),
 ('hardware','order_items','规模·订单明细','','','warehouse_id',['order_id','line_no','warehouse_id','sku','quantity','unit_price','line_total']),
 ('hardware','stock_movements','规模·出入库流水','','','warehouse_id',['movement_id','warehouse_id','location_id','sku','movement_type','quantity','occurred_at','document_no','order_id','line_no','operator_employee_id','reason']),
 ('hardware','after_sales','规模·售后工单','','','warehouse_id',['ticket_id','warehouse_id','order_id','line_no','sku','customer_name','opened_at','status','issue_type','description','warranty_expires','handling_note']),
]

def main():
 global TOKEN
 TOKEN=call('POST','/login',{'username':'admin','password':ENV['BOOTSTRAP_ADMIN_PASSWORD']})['token']
 sources={};existing_sources=call('GET','/admin/datasources')
 for kind in ['hr','hardware']:
  prefix='NOVEX_'+kind.upper()+'_SCALE_';name='Scale '+kind.upper()+' ≥2GiB'
  matches=[d for d in existing_sources if d['name']==name]
  if matches:source=matches[0]
  else:source=call('POST','/admin/datasources',{'name':name,'host':ENV[prefix+'HOST'],'port':int(ENV[prefix+'PORT']),'database':ENV[prefix+'DATABASE'],'username':ENV[prefix+'USER'],'password':ENV[prefix+'PASSWORD'],'sslMode':'disable','tenantId':''})
  if source['database']!=ENV[prefix+'DATABASE']:raise RuntimeError('Existing scale source has unexpected database')
  call('POST','/admin/datasources/'+source['id']+'/introspect',{})
  sources[kind]=source['id']
 existing_resources={r['key']:r for r in call('GET','/admin/resources')};resources={}
 for kind,table,name,owner,dept,warehouse,columns in RESOURCE_SPECS:
  key='scale_'+('hr' if kind=='hr' else 'wms')+'_'+table
  resource=existing_resources.get(key)
  if not resource:resource=call('POST','/admin/resources',{'name':name,'key':key,'datasourceId':sources[kind],'schema':'public','table':table,'tenantColumn':'tenant_id','ownerColumn':owner,'departmentColumn':dept,'warehouseColumn':warehouse,'identityRequired':True,'columns':columns})
  if resource['datasourceId']!=sources[kind]:raise RuntimeError('Existing resource belongs to another source')
  resources[key]=resource
 existing_roles={r['code']:r for r in call('GET','/admin/roles')};roles={}
 for code,name in [('scale_employee','规模·员工'),('scale_dept','规模·部门薪资'),('scale_payroll','规模·HR薪资'),('scale_products','规模·产品客服'),('scale_warehouse','规模·仓库'),('scale_procurement','规模·采购审计')]:
  roles[code]=existing_roles.get(code) or call('POST','/admin/roles',{'name':name,'code':code})
 userspecs=[
 ('scale_admin','规模知识发布管理员','SC001','D020','',[],['admin','novex_governance']),
 ('scale_a','Alice规模员工','SC001','D001','E001',[],['scale_employee','scale_products']),
 ('scale_b','Bob规模主管','SC001','D001','E002',['W1'],['scale_employee','scale_dept','scale_products','scale_warehouse']),
 ('scale_c','Carol薪资采购专员','SC001','D020','E5000',['W1','W2','W3','W4','W5'],['scale_employee','scale_payroll','scale_products','scale_procurement','scale_warehouse']),
 ('scale_x','其他企业员工','SC002','D001','E001',['W1'],['scale_employee','scale_products','scale_warehouse'])]
 all_roles={**existing_roles,**roles};existing_users={u['username']:u for u in call('GET','/admin/users')};users={}
 existing_identities={(i['userId'],i['datasourceId']):i for i in call('GET','/admin/identities')}
 for username,nickname,tenant,dept,employee,warehouses,role_codes in userspecs:
  role_ids=[all_roles[code]['id'] for code in role_codes];u=existing_users.get(username)
  if not u:
   created=call('POST','/admin/users',{'username':username,'nickname':nickname,'password':ENV['BOOTSTRAP_ADMIN_PASSWORD'],'tenantId':tenant,'departmentId':dept,'roleIds':role_ids});u={'id':created['id'],'username':username,'tenantId':tenant,'departmentId':dept,'roles':[all_roles[code] for code in role_codes]}
  if u['tenantId']!=tenant or u['departmentId']!=dept:raise RuntimeError('Scale account has unexpected business identity: '+username)
  if not set(role_ids).issubset({r['id'] for r in u['roles']}):raise RuntimeError('Scale account role configuration changed; refusing to overwrite: '+username)
  users[username]={'id':u['id'],'tenantId':tenant,'departmentId':dept,'employeeId':employee,'warehouseIds':warehouses,'roles':role_codes}
  if username=='scale_admin':continue
  for kind,source_id in sources.items():
   if (u['id'],source_id) not in existing_identities:call('POST','/admin/identities',{'userId':u['id'],'datasourceId':source_id,'tenantId':tenant,'externalTenantId':tenant,'employeeId':employee,'departmentId':dept,'warehouseIds':warehouses})
 grants=[]
 def grant(role,key,scope,selection):grants.append({'roleId':roles[role]['id'],'resourceId':resources[key]['id'],'scope':scope,'fields':selection})
 specs={('scale_'+('hr' if k=='hr' else 'wms')+'_'+table):columns for k,table,_,_,_,_,columns in RESOURCE_SPECS}
 for key in ['scale_hr_companies','scale_hr_departments']:grant('scale_employee',key,'tenant',fields(*specs[key]))
 grant('scale_employee','scale_hr_employees','tenant',fields('employee_id','employee_name','department_id','job_title','employment_status','hire_date','email'))
 for key in ['scale_hr_contracts','scale_hr_payroll','scale_hr_attendance','scale_hr_leave_requests']:
  visible=[x for x in specs[key] if x!='bank_account'];grant('scale_employee',key,'self',fields(*visible))
  full=fields(*visible)
  if key=='scale_hr_payroll':full['bank_account']='masked'
  grant('scale_payroll',key,'tenant',full)
 for key in ['scale_hr_attendance','scale_hr_leave_requests']:grant('scale_dept',key,'department',fields(*specs[key]))
 grant('scale_dept','scale_hr_payroll','department',fields('employee_id','employee_name','department_id','month','base','total','payment_status','payment_date','payroll_note'))
 grant('scale_products','scale_wms_products','tenant',fields('sku','name','category','spec','unit','sale_price','warranty_months','description'))
 grant('scale_procurement','scale_wms_products','tenant',fields(*specs['scale_wms_products']))
 for key in ['scale_wms_warehouses','scale_wms_locations','scale_wms_inventory','scale_wms_stock_movements','scale_wms_after_sales']:grant('scale_warehouse',key,'warehouse',fields(*specs[key]))
 grant('scale_warehouse','scale_wms_orders','warehouse',fields(*[x for x in specs['scale_wms_orders'] if x!='total_amount']))
 grant('scale_warehouse','scale_wms_order_items','warehouse',fields(*[x for x in specs['scale_wms_order_items'] if x not in ('unit_price','line_total')]))
 for key in ['scale_wms_orders','scale_wms_order_items']:grant('scale_procurement',key,'tenant',fields(*specs[key]))
 existing_grants=call('GET','/admin/grants')
 signature=lambda g:json.dumps({k:g[k] for k in ('roleId','resourceId','scope','fields')},sort_keys=True)
 known={signature(g) for g in existing_grants}
 for g in grants:
  if signature(g) not in known:call('POST','/admin/grants',g)
 manifest={'sources':sources,'resources':{k:{'id':r['id'],'fields':r['columns']} for k,r in resources.items()},'users':users,'roles':{k:v['id'] for k,v in roles.items()},'passwordEnvironmentVariable':'BOOTSTRAP_ADMIN_PASSWORD'}
 out=ROOT/'.local/scale';out.mkdir(exist_ok=True,parents=True);(out/'registration.json').write_text(json.dumps(manifest,indent=2,ensure_ascii=False));print(json.dumps({'registered':True,'sources':len(sources),'resources':len(resources),'users':list(users),'grants':len(grants)},ensure_ascii=False))
if __name__=='__main__':main()
