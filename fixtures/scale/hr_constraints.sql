ALTER TABLE departments ADD CONSTRAINT departments_company_fk FOREIGN KEY(tenant_id) REFERENCES companies(tenant_id) NOT VALID;
ALTER TABLE employees ADD CONSTRAINT employees_department_fk FOREIGN KEY(tenant_id,department_id) REFERENCES departments(tenant_id,department_id) NOT VALID;
ALTER TABLE departments ADD CONSTRAINT departments_manager_fk FOREIGN KEY(tenant_id,manager_employee_id) REFERENCES employees(tenant_id,employee_id) NOT VALID;
ALTER TABLE contracts ADD CONSTRAINT contracts_employee_fk FOREIGN KEY(tenant_id,employee_id) REFERENCES employees(tenant_id,employee_id) NOT VALID;
ALTER TABLE payroll ADD CONSTRAINT payroll_employee_fk FOREIGN KEY(tenant_id,employee_id) REFERENCES employees(tenant_id,employee_id) NOT VALID;
ALTER TABLE attendance ADD CONSTRAINT attendance_employee_fk FOREIGN KEY(tenant_id,employee_id) REFERENCES employees(tenant_id,employee_id) NOT VALID;
ALTER TABLE leave_requests ADD CONSTRAINT leave_employee_fk FOREIGN KEY(tenant_id,employee_id) REFERENCES employees(tenant_id,employee_id) NOT VALID;
ALTER TABLE leave_requests ADD CONSTRAINT leave_approver_fk FOREIGN KEY(tenant_id,approver_employee_id) REFERENCES employees(tenant_id,employee_id) NOT VALID;
