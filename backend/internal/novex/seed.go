package novex

import (
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"os"
	"strconv"
)

func (s *Server) Seed(ctx context.Context) error {
	password := os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	if len(password) < 12 || len(password) > 72 {
		return fmt.Errorf("BOOTSTRAP_ADMIN_PASSWORD must be explicitly configured with 12–72 bytes")
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if e != nil {
		return e
	}
	roles := []Role{{9101, "Novex 普通员工", "novex_employee"}, {9102, "Novex 部门薪资", "novex_dept_payroll"}, {9103, "Novex 薪资专员", "novex_payroll"}, {9104, "Novex 产品客服", "novex_products"}, {9105, "Novex 仓库人员", "novex_warehouse"}, {9106, "Novex 授权治理", "novex_governance"}}
	for _, r := range roles {
		if _, e = s.DB.ExecContext(ctx, `INSERT INTO sys_role(id,name,code,data_scope,is_system,create_user,create_time) VALUES($1,$2,$3,4,false,1,now()) ON CONFLICT DO NOTHING`, r.ID, r.Name, r.Code); e != nil {
			return e
		}
	}
	users := []struct {
		id                                     int64
		username, name, tenant, dept, employee string
		roles                                  []int64
		warehouses                             []string
	}{
		{9201, "a", "Alice / A 普通员工", "acme", "sales", "E001", []int64{9101, 9104}, nil},
		{9202, "b", "Bob / B 部门薪资与仓库", "acme", "sales", "E002", []int64{9101, 9102, 9104, 9105}, []string{"W1"}},
		{9203, "c", "Carol / C 薪资专员", "acme", "hr", "E003", []int64{9101, 9103, 9104}, nil},
		{9204, "d", "David / D 普通员工", "acme", "sales", "E004", []int64{9101, 9104}, nil},
		{9205, "e", "Evan / E 研发与二号仓", "acme", "rd", "E005", []int64{9101, 9104, 9105}, []string{"W2"}},
		{9206, "x", "Xavier / X 外部租户", "globex", "sales", "E001", []int64{9101, 9104, 9105}, []string{"W1"}},
	}
	for _, u := range users {
		_, e = s.DB.ExecContext(ctx, `INSERT INTO sys_user(id,username,nickname,password,status,is_system,dept_id,create_user,create_time) VALUES($1,$2,$3,$4,1,false,1,1,now()) ON CONFLICT DO NOTHING`, u.id, u.username, u.name, "{bcrypt}"+string(hash))
		if e != nil {
			return e
		}
		_, e = s.DB.ExecContext(ctx, `INSERT INTO novex_users(user_id,tenant_id,department_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, u.id, u.tenant, u.dept)
		if e != nil {
			return e
		}
		for _, role := range u.roles {
			_, e = s.DB.ExecContext(ctx, `INSERT INTO sys_user_role(id,user_id,role_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, u.id*10000+role, u.id, role)
			if e != nil {
				return e
			}
		}
	}
	_, e = s.DB.ExecContext(ctx, `INSERT INTO sys_user_role(id,user_id,role_id) SELECT 99106,id,9106 FROM sys_user WHERE username='admin' ON CONFLICT DO NOTHING`)
	if e != nil {
		return e
	}
	for _, spec := range []struct{ id, prefix, name, db, user string }{{"demo-hr", "NOVEX_HR_", "独立 HR 数据库", "novex-hr", "novex_hr_read"}, {"demo-hardware", "NOVEX_HARDWARE_", "独立五金产品与仓库库", "novex-hardware", "novex_hardware_read"}} {
		port, _ := strconv.Atoi(env(spec.prefix+"PORT", "5432"))
		d := Datasource{ID: spec.id, Name: spec.name, Host: env(spec.prefix+"HOST", "127.0.0.1"), Port: port, Database: env(spec.prefix+"DATABASE", spec.db), Username: env(spec.prefix+"USER", spec.user), Password: os.Getenv(spec.prefix + "PASSWORD"), SSLMode: env(spec.prefix+"SSLMODE", "disable")}
		if d.Password == "" {
			return fmt.Errorf("%sPASSWORD required", spec.prefix)
		}
		if _, e = s.checkDatasource(ctx, d); e != nil {
			return fmt.Errorf("%s: %w", spec.id, e)
		}
		encrypted, e := encrypt(s.key, d.Password)
		if e != nil {
			return e
		}
		tables, e := s.scanSchema(ctx, d)
		if e != nil {
			return e
		}
		snap, _ := json.Marshal(tables)
		_, e = s.DB.ExecContext(ctx, `INSERT INTO novex_datasources(id,name,host,port,database_name,username,password_encrypted,ssl_mode,schema_snapshot) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb) ON CONFLICT(id) DO UPDATE SET schema_snapshot=excluded.schema_snapshot`, d.ID, d.Name, d.Host, d.Port, d.Database, d.Username, encrypted, d.SSLMode, string(snap))
		if e != nil {
			return e
		}
		for _, u := range users {
			warehouses := u.warehouses
			if warehouses == nil {
				warehouses = []string{}
			}
			j, _ := json.Marshal(warehouses)
			_, e = s.DB.ExecContext(ctx, `INSERT INTO novex_identities(id,user_id,datasource_id,tenant_id,employee_id,department_id,warehouse_ids,external_tenant_id) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$4) ON CONFLICT DO NOTHING`, fmt.Sprintf("identity-%d-%s", u.id, d.ID), u.id, d.ID, u.tenant, u.employee, u.dept, string(j))
			if e != nil {
				return e
			}
		}
	}
	resources := []Resource{
		{ID: "payroll", Key: "payroll", Name: "工资单 / HR", DatasourceID: "demo-hr", Schema: "public", Table: "novex_demo_payroll", TenantColumn: "tenant_id", OwnerColumn: "employee_id", DepartmentColumn: "department_id", IdentityRequired: true, Columns: []string{"employee_id", "employee_name", "department_id", "month", "base", "bonus", "total", "bank_account"}},
		{ID: "employees", Key: "employees", Name: "员工目录 / HR", DatasourceID: "demo-hr", Schema: "public", Table: "novex_demo_employees", TenantColumn: "tenant_id", OwnerColumn: "employee_id", DepartmentColumn: "department_id", IdentityRequired: true, Columns: []string{"employee_id", "name", "department_id", "title"}},
		{ID: "products", Key: "products", Name: "五金产品目录", DatasourceID: "demo-hardware", Schema: "public", Table: "novex_demo_products", TenantColumn: "tenant_id", IdentityRequired: true, Columns: []string{"sku", "name", "category", "spec", "sale_price", "cost_price", "supplier"}},
		{ID: "inventory", Key: "inventory", Name: "仓库库存 / WMS", DatasourceID: "demo-hardware", Schema: "public", Table: "novex_demo_inventory", TenantColumn: "tenant_id", WarehouseColumn: "warehouse_id", IdentityRequired: true, Columns: []string{"warehouse_id", "warehouse_name", "sku", "product_name", "stock", "reserved", "reorder_level"}},

		{ID: "warehouses", Key: "warehouses", Name: "仓库目录 / WMS", DatasourceID: "demo-hardware", Schema: "public", Table: "novex_demo_warehouses", TenantColumn: "tenant_id", WarehouseColumn: "warehouse_id", IdentityRequired: true, Columns: []string{"warehouse_id", "warehouse_name", "address", "manager_employee_id"}},
		{ID: "stock_movements", Key: "stock_movements", Name: "出入库流水 / WMS", DatasourceID: "demo-hardware", Schema: "public", Table: "novex_demo_stock_movements", TenantColumn: "tenant_id", WarehouseColumn: "warehouse_id", IdentityRequired: true, Columns: []string{"movement_id", "warehouse_id", "sku", "movement_type", "quantity", "occurred_at", "document_no", "operator_employee_id"}},
	}
	for _, r := range resources {
		if e = s.validateResource(ctx, &r); e != nil {
			return fmt.Errorf("seed resource %s: %w", r.Key, e)
		}
		j, _ := json.Marshal(r)
		_, e = s.DB.ExecContext(ctx, `INSERT INTO novex_resources(id,resource_key,name,datasource_id,config) VALUES($1,$2,$3,$4,$5::jsonb) ON CONFLICT DO NOTHING`, r.ID, r.Key, r.Name, r.DatasourceID, string(j))
		if e != nil {
			return e
		}
	}
	plain := func(fields ...string) map[string]string {
		m := map[string]string{}
		for _, f := range fields {
			m[f] = "plain"
		}
		return m
	}
	payrollAll := plain("employee_id", "employee_name", "department_id", "month", "base", "bonus", "total")
	payrollHR := plain("employee_id", "employee_name", "department_id", "month", "base", "bonus", "total")
	payrollHR["bank_account"] = "masked"
	grants := []Grant{{"seed-payroll-self", 9101, "payroll", "self", payrollAll}, {"seed-payroll-dept", 9102, "payroll", "department", plain("employee_id", "employee_name", "department_id", "month", "base", "total")}, {"seed-payroll-hr", 9103, "payroll", "tenant", payrollHR}, {"seed-directory", 9101, "employees", "tenant", plain("employee_id", "name", "department_id", "title")}, {"seed-products", 9104, "products", "tenant", plain("sku", "name", "category", "spec", "sale_price")}, {"seed-inventory", 9105, "inventory", "warehouse", plain("warehouse_id", "warehouse_name", "sku", "product_name", "stock", "reserved", "reorder_level")}}

	grants = append(grants,
		Grant{ID: "seed-warehouses", RoleID: 9105, ResourceID: "warehouses", Scope: "warehouse", Fields: plain("warehouse_id", "warehouse_name", "address", "manager_employee_id")},
		Grant{ID: "seed-stock-movements", RoleID: 9105, ResourceID: "stock_movements", Scope: "warehouse", Fields: plain("movement_id", "warehouse_id", "sku", "movement_type", "quantity", "occurred_at", "document_no", "operator_employee_id")},
	)
	for _, g := range grants {
		j, _ := json.Marshal(g.Fields)
		_, e = s.DB.ExecContext(ctx, `INSERT INTO novex_grants(id,role_id,resource_id,scope,fields) VALUES($1,$2,$3,$4,$5::jsonb) ON CONFLICT DO NOTHING`, g.ID, g.RoleID, g.ResourceID, g.Scope, string(j))
		if e != nil {
			return e
		}
	}
	docs := []struct {
		id, title, scope, dept, tenant, body string
		owner                                int64
	}{
		{"demo-product-guide", "五金产品选型与售后指南", "tenant", "sales", "acme", "# 五金产品选型与售后指南\n\n304不锈钢螺栓适用于普通潮湿环境，沿海高盐雾环境应评估316材质。HW-001是M8×30规格，100枚/包。\n\n6204-2RS轴承尺寸20×47×14mm，2RS代表双面橡胶密封，安装时不可通过滚动体传递敲击力。\n\n18V电动扭矩扳手HW-003可调20–200Nm，随附双电池。使用前检查套筒和扭矩设定。\n\n产品保修12个月，耗材和误操作损伤不在保修范围。价格与现货必须实时查询产品库和仓库，不依据本文件猜测。", 9201},
		{"demo-payroll-policy", "员工工资查询说明", "tenant", "hr", "acme", "# 工资查询\n\n工资于次月10日发放。本人可查询个人工资；部门薪资管理员仅能查询被授权部门的基本工资和总额。奖金明细不因管理岗位自动开放。薪资专员可在本租户授权范围查询工资。工资金额必须调用HR工具。", 9203},
		{"demo-hr-private", "人事部门内部薪酬复核流程", "department", "hr", "acme", "# 人事部门内部复核\n\n本文件仅供人事部门。内部复核标记：HR-PRIVATE-CANARY-7391。工资变更需要两名授权人员复核，银行信息不得在客服回答中明文展示。", 9203},
		{"demo-a-private", "Alice个人工作笔记", "self", "sales", "acme", "# Alice个人笔记\n\n个人工作提醒：周五跟进客户的轴承替换需求。私人标记：ALICE-PRIVATE-2846。", 9201},
		{"demo-other-tenant", "其他企业私有指南", "tenant", "sales", "globex", "# Globex内部指南\n\n此内容仅属于Globex。租户隔离标记：GLOBEX-ONLY-5639。", 9206},
	}
	for _, d := range docs {
		_, e = s.DB.ExecContext(ctx, `INSERT INTO novex_documents(id,title,file_name,scope,owner_id,tenant_id,department_id,markdown) VALUES($1,$2,$1||'.md',$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, d.id, d.title, d.scope, d.owner, d.tenant, d.dept, d.body)
		if e != nil {
			return e
		}
	}
	return nil
}
