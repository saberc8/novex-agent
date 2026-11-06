-- Synthetic demonstration data only. Safe to reapply; never deletes existing data.
CREATE TABLE IF NOT EXISTS public.novex_demo_employees (
 tenant_id text NOT NULL, employee_id text NOT NULL, name text NOT NULL,
 department_id text NOT NULL, title text NOT NULL,
 PRIMARY KEY(tenant_id,employee_id)
);
CREATE TABLE IF NOT EXISTS public.novex_demo_payroll (
 tenant_id text NOT NULL, employee_id text NOT NULL, employee_name text NOT NULL,
 department_id text NOT NULL, month text NOT NULL,
 base numeric(12,2) NOT NULL, bonus numeric(12,2) NOT NULL, total numeric(12,2) NOT NULL,
 bank_account text NOT NULL, PRIMARY KEY(tenant_id,employee_id,month)
);
INSERT INTO public.novex_demo_employees VALUES
 ('acme','E001','Alice / A','sales','销售顾问'),('acme','E002','Bob / B','sales','销售主管'),
 ('acme','E003','Carol / C','hr','薪资专员'),('acme','E004','David / D','sales','销售顾问'),
 ('acme','E005','Evan / E','rd','研发工程师'),('globex','E001','Xavier / X','sales','外部租户员工')
 ON CONFLICT DO NOTHING;
INSERT INTO public.novex_demo_payroll VALUES
 ('acme','E001','Alice / A','sales','2026-10',8000,1000,9000,'DEMO-0001'),
 ('acme','E002','Bob / B','sales','2026-10',15000,3000,18000,'DEMO-0002'),
 ('acme','E003','Carol / C','hr','2026-10',20000,5000,25000,'DEMO-0003'),
 ('acme','E004','David / D','sales','2026-10',10000,2000,12000,'DEMO-0004'),
 ('acme','E005','Evan / E','rd','2026-10',20000,3000,23000,'DEMO-0005'),
 ('globex','E001','Xavier / X','sales','2026-10',60000,6000,66000,'DEMO-OTHER')
 ON CONFLICT DO NOTHING;
