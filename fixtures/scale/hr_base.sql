INSERT INTO companies SELECT 'SC'||lpad(n::text,3,'0'),'规模演示企业'||lpad(n::text,3,'0'),'制造与技术服务',(ARRAY['上海','深圳','苏州','杭州','宁波'])[(n-1)%5+1],date '2015-01-01'+n*30 FROM generate_series(1,20)n ON CONFLICT DO NOTHING;
INSERT INTO departments
SELECT c.tenant_id,'D'||lpad(d::text,3,'0'),CASE WHEN d=1 THEN '销售部' WHEN d=20 THEN '人事与财务部' ELSE '业务事业部'||d END,NULL,'E'||CASE WHEN (d-1)*250+1<1000 THEN lpad(((d-1)*250+1)::text,3,'0') ELSE ((d-1)*250+1)::text END,'CC-'||lpad(d::text,3,'0') FROM companies c CROSS JOIN generate_series(1,20)d ON CONFLICT DO NOTHING;
INSERT INTO employees
SELECT 'SC'||lpad(((n-1)/5000+1)::text,3,'0'),'E'||CASE WHEN (n-1)%5000+1<1000 THEN lpad(((n-1)%5000+1)::text,3,'0') ELSE ((n-1)%5000+1)::text END,n,
CASE WHEN (n-1)%5000+1=1 THEN 'Alice规模员工' WHEN (n-1)%5000+1=2 THEN 'Bob规模主管' WHEN (n-1)%5000+1=5000 THEN 'Carol薪资采购专员' ELSE '合成员工'||n END,
'D'||lpad((((n-1)%5000)/250+1)::text,3,'0'),CASE WHEN (n-1)%250=0 THEN '部门负责人' WHEN (n-1)%5000+1=2 THEN '薪资与仓储主管' ELSE '业务专员' END,'active',date '2020-01-01'+(n%1800),date '1978-01-01'+(n%9000),
'employee-'||n||'@example.invalid','DEMO-PHONE-'||n,'DEMO-BANK-'||n,'合成地址：业务园区'||(n%80+1)||'号楼' FROM generate_series(1,100000)n ON CONFLICT DO NOTHING;
INSERT INTO contracts SELECT tenant_id,'CT-'||employee_id,employee_id,department_id,hire_date,date '2029-12-31','fixed-term',
CASE WHEN (seed_no-1)%5000+1=1 THEN 8000 WHEN (seed_no-1)%5000+1=2 THEN 15000 WHEN (seed_no-1)%5000+1=5000 THEN 20000 ELSE 6500+(((seed_no-1)%5000+1)%90)*120 END,
'上海总部及经批准的远程工作地点','标准工时；试用期与培训义务按合同附件执行；绩效奖金按月复核。该合同为合成测试数据。' FROM employees ON CONFLICT DO NOTHING;
INSERT INTO payroll
SELECT tenant_id,employee_id,employee_name,department_id,to_char(pay_month,'YYYY-MM'),base_salary,bonus,overtime_pay,gross,round(base_salary*.1,2),round(greatest((gross-base_salary*.1-5000)*.03,0),2),gross-round(base_salary*.1,2)-round(greatest((gross-base_salary*.1-5000)*.03,0),2),bank_account,
CASE WHEN (pay_month+interval '1 month 9 days')::date<=date '2026-11-06' THEN 'paid' ELSE 'scheduled' END,(pay_month+interval '1 month 9 days')::date,'基本薪资、绩效奖金与加班结算已核对；实际工资以授权工资单为准，全部为合成记录。'
FROM (SELECT e.tenant_id,e.employee_id,e.employee_name,e.department_id,e.bank_account,c.base_salary,m.pay_month,b.bonus,b.overtime_pay,c.base_salary+b.bonus+b.overtime_pay AS gross FROM employees e JOIN contracts c USING(tenant_id,employee_id) CROSS JOIN (SELECT (date '2025-11-01'+m*interval '1 month')::date pay_month FROM generate_series(0,11)m)m
CROSS JOIN LATERAL (SELECT CASE WHEN (e.seed_no-1)%5000+1=1 THEN 1000 WHEN (e.seed_no-1)%5000+1=2 THEN 3000 WHEN (e.seed_no-1)%5000+1=5000 THEN 5000 ELSE 800+(e.seed_no%12)*100 END bonus,CASE WHEN (e.seed_no-1)%5000+1 IN(1,2,5000) THEN 0 ELSE (e.seed_no%4)*60 END overtime_pay)b) p ON CONFLICT DO NOTHING;
INSERT INTO leave_requests SELECT e.tenant_id,'LEAVE-'||e.employee_id,e.employee_id,e.department_id,date '2026-10-19',date '2026-10-20',2,'annual','approved',d.manager_employee_id,'已批准的年度休假，用于验证考勤与请假关联；合成业务记录。' FROM employees e JOIN departments d USING(tenant_id,department_id) WHERE e.seed_no%10=0 ON CONFLICT DO NOTHING;
