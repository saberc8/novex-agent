-- Read-only integrity checks; expected violations = 0 for every statement.

SELECT current_database(),pg_database_size(current_database()) AS size_bytes;

SELECT 'employee_department_mismatch' AS check_name,count(*) AS violations FROM employees e LEFT JOIN departments d USING(tenant_id,department_id) WHERE d.department_id IS NULL;

SELECT 'payroll_employee_or_department_mismatch' AS check_name,count(*) AS violations FROM payroll p JOIN employees e USING(tenant_id,employee_id) WHERE p.department_id<>e.department_id;

SELECT 'payroll_calculation_mismatch' AS check_name,count(*) AS violations FROM payroll WHERE total<>base+bonus+overtime_pay OR net_pay<>total-social_insurance-tax;

SELECT 'attendance_employee_department_mismatch' AS check_name,count(*) AS violations FROM attendance a JOIN employees e USING(tenant_id,employee_id) WHERE a.department_id<>e.department_id;

SELECT 'leave_attendance_mismatch' AS check_name,count(*) AS violations FROM leave_requests l JOIN attendance a USING(tenant_id,employee_id) WHERE a.work_date BETWEEN l.start_date AND l.end_date AND l.status='approved' AND a.status<>'leave';

SELECT 'unvalidated_foreign_keys' AS check_name,count(*) AS violations FROM pg_constraint WHERE contype='f' AND NOT convalidated;
