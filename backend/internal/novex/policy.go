package novex

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"sort"
	"strings"
	"time"
)

var ErrDenied = errors.New("没有访问该资源或请求字段的权限")

func (s *Server) identity(ctx context.Context, u *User, ds string) (Identity, error) {
	var i Identity
	var warehouses []byte
	e := s.DB.QueryRowContext(ctx, `SELECT id,user_id,datasource_id,tenant_id,employee_id,department_id,warehouse_ids,external_tenant_id FROM novex_identities WHERE user_id=$1 AND datasource_id=$2 AND tenant_id=$3`, u.ID, ds, u.TenantID).Scan(&i.ID, &i.UserID, &i.DatasourceID, &i.TenantID, &i.EmployeeID, &i.DepartmentID, &warehouses, &i.ExternalTenantID)
	if e == nil {
		e = json.Unmarshal(warehouses, &i.WarehouseIDs)
	}
	return i, e
}
func (s *Server) grants(ctx context.Context, uid int64, rid string) ([]Grant, error) {
	rows, e := s.DB.QueryContext(ctx, `SELECT g.id,g.role_id,g.resource_id,g.scope,g.fields FROM novex_grants g JOIN sys_user_role ur ON ur.role_id=g.role_id WHERE ur.user_id=$1 AND g.resource_id=$2 ORDER BY g.id`, uid, rid)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Grant{}
	for rows.Next() {
		var g Grant
		var fields []byte
		if e = rows.Scan(&g.ID, &g.RoleID, &g.ResourceID, &g.Scope, &fields); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(fields, &g.Fields); e != nil {
			return nil, e
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
func scopeSQL(g Grant, r Resource, i Identity, args *[]any) (string, error) {
	add := func(v any) string { *args = append(*args, v); return fmt.Sprintf("$%d", len(*args)) }
	col := func(v string) string { return "CAST(" + pq.QuoteIdentifier(v) + " AS text)" }
	switch g.Scope {
	case "tenant":
		return "TRUE", nil
	case "self":
		if r.OwnerColumn != "" && i.EmployeeID != "" {
			return col(r.OwnerColumn) + "=" + add(i.EmployeeID), nil
		}
	case "department":
		if r.DepartmentColumn != "" && i.DepartmentID != "" {
			return col(r.DepartmentColumn) + "=" + add(i.DepartmentID), nil
		}
	case "warehouse":
		if r.WarehouseColumn != "" && len(i.WarehouseIDs) > 0 {
			return col(r.WarehouseColumn) + "=ANY(" + add(pq.Array(i.WarehouseIDs)) + "::text[])", nil
		}
	}
	return "FALSE", nil
}
func stringValue(v any) string {
	if v == nil {
		return ""
	}
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return fmt.Sprint(v)
}
func matches(g Grant, r Resource, i Identity, row map[string]any) bool {
	switch g.Scope {
	case "tenant":
		return true
	case "self":
		return r.OwnerColumn != "" && i.EmployeeID != "" && stringValue(row[r.OwnerColumn]) == i.EmployeeID
	case "department":
		return r.DepartmentColumn != "" && i.DepartmentID != "" && stringValue(row[r.DepartmentColumn]) == i.DepartmentID
	case "warehouse":
		return r.WarehouseColumn != "" && contains(i.WarehouseIDs, stringValue(row[r.WarehouseColumn]))
	}
	return false
}
func fieldMode(gs []Grant, r Resource, i Identity, row map[string]any, field string) string {
	mode := ""
	for _, g := range gs {
		if !matches(g, r, i, row) {
			continue
		}
		if g.Fields[field] == "plain" {
			return "plain"
		}
		if g.Fields[field] == "masked" {
			mode = "masked"
		}
	}
	return mode
}
func fieldPlainForEveryGrant(gs []Grant, field string) bool {
	if len(gs) == 0 {
		return false
	}
	for _, g := range gs {
		if g.Fields[field] != "plain" {
			return false
		}
	}
	return true
}
func normalizeValue(v any) any {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}

// A global reader is still limited to the registered schema. Include the
// registered tenant discriminator so equal employee/SKU IDs remain distinguishable.
func globalReadResource(r Resource) Resource {
	r.Columns = append([]string{}, r.Columns...)
	if r.TenantColumn != "" && !contains(r.Columns, r.TenantColumn) {
		r.Columns = append(r.Columns, r.TenantColumn)
	}
	return r
}

func globalReadGrant(r Resource) Grant {
	fields := map[string]string{}
	for _, column := range r.Columns {
		fields[column] = "plain"
	}
	// This grant is local to an already-authorized global query. It is never
	// persisted or used to infer global access from an ordinary tenant grant.
	return Grant{Scope: "tenant", Fields: fields}
}

func globalVisibleResource(r Resource, d Datasource) map[string]any {
	r = globalReadResource(r)
	return map[string]any{
		"id": r.ID, "name": r.Name, "key": r.Key, "datasourceId": r.DatasourceID, "datasourceName": d.Name,
		"columns": r.Columns, "tenantColumn": r.TenantColumn, "scopes": []string{"global"},
		"actor": map[string]any{"accessMode": "global", "identityRequired": false},
	}
}

func (s *Server) Query(ctx context.Context, u *User, q Query) (map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.queryUnlocked(ctx, u, q)
}
func (s *Server) queryUnlocked(ctx context.Context, u *User, q Query) (result map[string]any, err error) {
	defer func() {
		s.audit(context.Background(), u.ID, "resource.query", q.ResourceID, err == nil, func() string {
			if err != nil {
				return "query denied or failed"
			}
			if globalDataAccess(u) {
				return "explicit global read of registered rows and fields"
			}
			return "authorized rows and fields only"
		}())
	}()
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	fresh, e := s.loadUser(ctx, u.ID)
	if e != nil || fresh.TenantID != u.TenantID {
		return nil, ErrDenied
	}
	u = fresh
	r, e := s.resource(ctx, q.ResourceID)
	if e != nil {
		return nil, ErrDenied
	}
	if e = s.validateResource(ctx, &r); e != nil {
		return nil, fmt.Errorf("资源schema已变化，请管理员重新扫描注册")
	}
	global := globalDataAccess(u)
	var i Identity
	var gs []Grant
	if global {
		r = globalReadResource(r)
		gs = []Grant{globalReadGrant(r)}
	} else {
		i, e = s.identity(ctx, u, r.DatasourceID)
		if e != nil || i.TenantID != u.TenantID {
			return nil, fmt.Errorf("当前账号尚未绑定该数据源的业务身份")
		}
		gs, e = s.grants(ctx, u.ID, r.ID)
		if e != nil {
			return nil, e
		}
		if len(gs) == 0 {
			return nil, ErrDenied
		}
	}
	allowed := map[string]bool{}
	for _, g := range gs {
		for f, m := range g.Fields {
			if contains(r.Columns, f) && (m == "plain" || m == "masked") {
				allowed[f] = true
			}
		}
	}
	fields := q.Fields
	if len(fields) == 0 {
		for _, f := range r.Columns {
			if allowed[f] {
				fields = append(fields, f)
			}
		}
	}
	if len(fields) == 0 {
		return nil, ErrDenied
	}
	for _, f := range fields {
		if !allowed[f] {
			return nil, ErrDenied
		}
	}
	if q.Limit <= 0 {
		q.Limit = 20
	}
	if q.Limit > 100 {
		q.Limit = 100
	}
	if len(q.Filters) > 10 {
		return nil, fmt.Errorf("筛选条件最多10个")
	}
	d, e := s.datasource(ctx, r.DatasourceID)
	if e != nil {
		return nil, e
	}
	if !global && d.TenantID != "" && d.TenantID != u.TenantID {
		return nil, ErrDenied
	}
	args := []any{}
	clauses := []string{}
	if !global && r.TenantColumn != "" {
		if i.ExternalTenantID == "" {
			return nil, ErrDenied
		}
		args = append(args, i.ExternalTenantID)
		clauses = append(clauses, "CAST("+pq.QuoteIdentifier(r.TenantColumn)+" AS text)=$1")
	} else if !global && d.TenantID != u.TenantID {
		return nil, ErrDenied
	}
	scopes := []string{}
	for _, g := range gs {
		expr, _ := scopeSQL(g, r, i, &args)
		scopes = append(scopes, expr)
	}
	clauses = append(clauses, "("+strings.Join(scopes, " OR ")+")")
	for _, f := range q.Filters {
		if !contains(r.Columns, f.Field) || !fieldPlainForEveryGrant(gs, f.Field) {
			return nil, fmt.Errorf("不能使用未完整授权的字段筛选")
		}
		args = append(args, f.Value)
		p := fmt.Sprintf("$%d", len(args))
		col := pq.QuoteIdentifier(f.Field)
		switch f.Op {
		case "eq":
			clauses = append(clauses, col+"="+p)
		case "gte":
			clauses = append(clauses, col+">="+p)
		case "lte":
			clauses = append(clauses, col+"<="+p)
		case "contains":
			args[len(args)-1] = "%" + strings.ReplaceAll(strings.ReplaceAll(stringValue(f.Value), "%", "\\%"), "_", "\\_") + "%"
			clauses = append(clauses, "CAST("+col+" AS text) ILIKE "+p)
		default:
			return nil, fmt.Errorf("不支持的筛选运算")
		}
	}
	internal := append([]string{}, fields...)
	for _, f := range []string{r.TenantColumn, r.OwnerColumn, r.DepartmentColumn, r.WarehouseColumn} {
		if f != "" && !contains(internal, f) {
			internal = append(internal, f)
		}
	}
	selects := []string{}
	for _, f := range internal {
		selects = append(selects, pq.QuoteIdentifier(f))
	}
	order := ""
	if q.Sort != nil {
		if !contains(r.Columns, q.Sort.Field) || !fieldPlainForEveryGrant(gs, q.Sort.Field) {
			return nil, fmt.Errorf("不能使用未完整授权的字段排序")
		}
		direction := strings.ToUpper(q.Sort.Direction)
		if direction != "ASC" && direction != "DESC" {
			return nil, fmt.Errorf("排序方向必须为asc或desc")
		}
		order = " ORDER BY " + pq.QuoteIdentifier(q.Sort.Field) + " " + direction
	}
	args = append(args, q.Limit+1)
	sqlText := "SELECT " + strings.Join(selects, ",") + " FROM " + pq.QuoteIdentifier(r.Schema) + "." + pq.QuoteIdentifier(r.Table) + " WHERE " + strings.Join(clauses, " AND ") + order + fmt.Sprintf(" LIMIT $%d", len(args))
	db, e := s.external(ctx, d)
	if e != nil {
		return nil, e
	}
	defer db.Close()
	tx, e := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SET LOCAL statement_timeout='5000ms'`); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, sqlText, args...)
	if e != nil {
		return nil, fmt.Errorf("查询失败，请检查字段类型或schema")
	}
	defer rows.Close()
	output := []map[string]any{}
	scanned := 0
	for rows.Next() {
		scanned++
		values := make([]any, len(internal))
		ptrs := make([]any, len(internal))
		for n := range ptrs {
			ptrs[n] = &values[n]
		}
		if e = rows.Scan(ptrs...); e != nil {
			return nil, e
		}
		raw := map[string]any{}
		for n, col := range internal {
			raw[col] = normalizeValue(values[n])
		}
		if !global && r.TenantColumn != "" && stringValue(raw[r.TenantColumn]) != i.ExternalTenantID {
			return nil, ErrDenied
		}
		row := map[string]any{}
		for _, f := range fields {
			switch fieldMode(gs, r, i, raw, f) {
			case "plain":
				row[f] = raw[f]
			case "masked":
				row[f] = "***"
			}
		}
		if len(row) > 0 {
			output = append(output, row)
		}
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	hasMore := scanned > q.Limit
	if len(output) > q.Limit {
		output = output[:q.Limit]
	}
	explanation := "强制租户与外部身份映射；每行字段仅来自同时匹配该行的授权。筛选与排序字段须在所有有效grant中明文可读。"
	if global {
		explanation = "当前账号已显式授予全局数据读取角色，可跨租户读取本资源已注册字段，无需员工身份映射。请用租户列区分同编号业务记录；仅提供只读查询。"
	}
	explanation += "hasMore=true时仅为部分记录，禁止将其合计或数量声称为全量统计；请收窄查询范围。"
	return map[string]any{"rows": output, "columns": fields, "hasMore": hasMore, "rowLimit": q.Limit, "resource": map[string]any{"id": r.ID, "key": r.Key, "name": r.Name, "datasourceId": r.DatasourceID, "datasourceName": d.Name, "tenantColumn": r.TenantColumn}, "policyVersion": u.PolicyVersion, "explanation": explanation}, nil
}
func (s *Server) queryHTTP(c *gin.Context) {
	var q Query
	if !bind(c, &q) {
		return
	}
	q.UserID = actor(c).ID
	out, e := s.Query(c, actor(c), q)
	if e != nil {
		fail(c, 403, e.Error())
		return
	}
	ok(c, out)
}
func (s *Server) preview(c *gin.Context) {
	if !governance(actor(c)) {
		fail(c, 403, "权限预览需要授权治理角色")
		return
	}
	var q Query
	if !bind(c, &q) {
		return
	}
	u, e := s.loadUser(c, q.UserID)
	if e != nil {
		fail(c, 404, "账号不存在")
		return
	}
	out, e := s.Query(c, u, q)
	if e != nil {
		fail(c, 403, e.Error())
		return
	}
	if rows, okRows := out["rows"].([]map[string]any); okRows {
		for _, row := range rows {
			for field, value := range row {
				if value != "***" {
					row[field] = "[可读]"
				}
			}
		}
	}
	out["explanation"] = "权限预览：业务值已隐藏，仅展示允许字段和行数；查看真实数据需登录目标账号。"
	ok(c, out)
}
func (s *Server) visible(ctx context.Context, u *User) ([]map[string]any, error) {
	resources, e := s.resources(ctx)
	if e != nil {
		return nil, e
	}
	out := []map[string]any{}
	for _, r := range resources {
		d, dsErr := s.datasource(ctx, r.DatasourceID)
		if dsErr != nil {
			continue
		}
		if globalDataAccess(u) {
			out = append(out, globalVisibleResource(r, d))
			continue
		}
		if d.TenantID != "" && d.TenantID != u.TenantID {
			continue
		}
		identity, identityErr := s.identity(ctx, u, r.DatasourceID)
		if identityErr != nil {
			continue
		}
		gs, e := s.grants(ctx, u.ID, r.ID)
		if e != nil || len(gs) == 0 {
			continue
		}
		fields := map[string]bool{}
		scopes := []string{}
		for _, g := range gs {
			if !contains(scopes, g.Scope) {
				scopes = append(scopes, g.Scope)
			}
			for f := range g.Fields {
				if contains(r.Columns, f) {
					fields[f] = true
				}
			}
		}
		cols := []string{}
		for f := range fields {
			cols = append(cols, f)
		}
		sort.Strings(cols)
		out = append(out, map[string]any{"id": r.ID, "name": r.Name, "key": r.Key, "columns": cols, "scopes": scopes, "actor": map[string]any{"employeeId": identity.EmployeeID, "departmentId": identity.DepartmentID, "warehouseIds": identity.WarehouseIDs, "externalTenantId": identity.ExternalTenantID}})
	}
	return out, nil
}
func (s *Server) visibleResources(c *gin.Context) {
	out, e := s.visible(c, actor(c))
	if e != nil {
		fail(c, 500, "读取失败")
		return
	}
	ok(c, out)
}
