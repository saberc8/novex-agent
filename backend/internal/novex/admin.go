package novex

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"strconv"
	"strings"
	ids "voc-go-backend/internal/infrastructure/id"
)

func governance(u *User) bool {
	for _, r := range u.Roles {
		if r.Code == "novex_governance" {
			return true
		}
	}
	return false
}
func (s *Server) overview(c *gin.Context) {
	out := map[string]any{}
	for k, t := range map[string]string{"datasources": "novex_datasources", "resources": "novex_resources", "documents": "novex_documents", "users": "novex_users", "runs": "novex_runs"} {
		var n int64
		if e := s.DB.QueryRowContext(c, "SELECT count(*) FROM "+t).Scan(&n); e != nil {
			fail(c, 500, "读取失败")
			return
		}
		out[k] = n
	}
	out["policyVersion"] = actor(c).PolicyVersion
	ok(c, out)
}
func (s *Server) listUsers(c *gin.Context) {
	rows, e := s.DB.QueryContext(c, `SELECT id FROM sys_user ORDER BY id`)
	if e != nil {
		fail(c, 500, "读取失败")
		return
	}
	defer rows.Close()
	out := []*User{}
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		u, e := s.loadSystemUser(c, id)
		if e == nil {
			out = append(out, u)
		}
	}
	ok(c, out)
}
func (s *Server) listRoles(c *gin.Context) {
	rows, e := s.DB.QueryContext(c, `SELECT id,name,code FROM sys_role ORDER BY id`)
	if e != nil {
		fail(c, 500, "读取失败")
		return
	}
	defer rows.Close()
	out := []Role{}
	for rows.Next() {
		var r Role
		_ = rows.Scan(&r.ID, &r.Name, &r.Code)
		out = append(out, r)
	}
	ok(c, out)
}
func (s *Server) createRole(c *gin.Context) {
	var r Role
	if !bind(c, &r) {
		return
	}
	if blank(r.Name) || blank(r.Code) || len(r.Name) > 80 || len(r.Code) > 30 {
		fail(c, 400, "角色名称与代码必填且代码不超过30字符")
		return
	}
	if !governance(actor(c)) {
		fail(c, 403, "角色编辑需要授权治理角色")
		return
	}
	r.ID = ids.Next()
	_, e := s.DB.ExecContext(c, `INSERT INTO sys_role(id,name,code,data_scope,is_system,create_user,create_time) VALUES($1,$2,$3,4,false,$4,now())`, r.ID, r.Name, r.Code, actor(c).ID)
	if e != nil {
		fail(c, 400, "角色名称或代码重复")
		return
	}
	ok(c, r)
}
func (s *Server) canAssign(c *gin.Context, roles []int64) bool {
	if len(roles) == 0 {
		return true
	}
	for _, rid := range roles {
		var code string
		if e := s.DB.QueryRowContext(c, `SELECT code FROM sys_role WHERE id=$1`, rid).Scan(&code); e != nil {
			return false
		}
		if !governance(actor(c)) && code != "novex_employee" && code != "general" {
			return false
		}
	}
	return true
}
func (s *Server) createUser(c *gin.Context) {
	var r struct {
		Username     string  `json:"username"`
		Nickname     string  `json:"nickname"`
		Password     string  `json:"password"`
		TenantID     string  `json:"tenantId"`
		DepartmentID string  `json:"departmentId"`
		RoleIDs      []int64 `json:"roleIds"`
	}
	if !bind(c, &r) {
		return
	}
	if blank(r.Username) || blank(r.TenantID) || len(r.Password) < 10 || len(r.Password) > 72 {
		fail(c, 400, "用户名、租户必填；密码需10–72字节")
		return
	}
	if !s.canAssign(c, r.RoleIDs) {
		fail(c, 403, "该角色不在当前管理员可授予范围")
		return
	}
	if !governance(actor(c)) && r.TenantID != actor(c).TenantID {
		fail(c, 403, "不能跨租户创建账号")
		return
	}
	if r.Nickname == "" {
		r.Nickname = r.Username
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(r.Password), bcrypt.DefaultCost)
	if e != nil {
		fail(c, 400, "密码不合法")
		return
	}
	uid := ids.Next()
	tx, e := s.DB.BeginTx(c, nil)
	if e != nil {
		fail(c, 500, "保存失败")
		return
	}
	defer tx.Rollback()
	_, e = tx.ExecContext(c, `INSERT INTO sys_user(id,username,nickname,password,status,is_system,dept_id,create_user,create_time) VALUES($1,$2,$3,$4,1,false,1,$5,now())`, uid, r.Username, r.Nickname, "{bcrypt}"+string(hash), actor(c).ID)
	if e == nil {
		_, e = tx.ExecContext(c, `INSERT INTO novex_users(user_id,tenant_id,department_id) VALUES($1,$2,$3)`, uid, r.TenantID, r.DepartmentID)
	}
	for _, rid := range r.RoleIDs {
		if e == nil {
			_, e = tx.ExecContext(c, `INSERT INTO sys_user_role(id,user_id,role_id) VALUES($1,$2,$3)`, ids.Next(), uid, rid)
		}
	}
	if e != nil || tx.Commit() != nil {
		fail(c, 400, "保存失败，用户名可能重复")
		return
	}
	s.audit(c, actor(c).ID, "user.create", strconv.FormatInt(uid, 10), true, "created with explicit roles")
	ok(c, gin.H{"id": uid})
}
func (s *Server) setRoles(c *gin.Context) {
	if !governance(actor(c)) {
		fail(c, 403, "角色变更需要授权治理角色，请使用系统用户管理")
		return
	}
	uid, e := numericID(c.Param("id"))
	if e != nil {
		fail(c, 400, "无效账号")
		return
	}
	var r struct {
		RoleIDs []int64 `json:"roleIds"`
	}
	if !bind(c, &r) {
		return
	}
	if !s.canAssign(c, r.RoleIDs) {
		fail(c, 403, "角色超出可授予范围")
		return
	}
	if uid == actor(c).ID {
		fail(c, 403, "禁止通过本接口修改自身管理权限")
		return
	}
	target, e := s.loadSystemUser(c, uid)
	if e != nil {
		fail(c, 404, "账号不存在")
		return
	}
	if target.IsSystem {
		fail(c, 400, "系统内置账号不允许重新分配角色")
		return
	}
	tx, e := s.DB.BeginTx(c, nil)
	if e != nil {
		fail(c, 500, "保存失败")
		return
	}
	defer tx.Rollback()
	_, e = tx.ExecContext(c, `DELETE FROM sys_user_role WHERE user_id=$1`, uid)
	for _, rid := range r.RoleIDs {
		if e == nil {
			_, e = tx.ExecContext(c, `INSERT INTO sys_user_role(id,user_id,role_id) VALUES($1,$2,$3)`, ids.Next(), uid, rid)
		}
	}
	if e != nil || tx.Commit() != nil {
		fail(c, 400, "保存失败")
		return
	}
	s.audit(c, actor(c).ID, "user.roles", c.Param("id"), true, "roles replaced")
	ok(c, gin.H{"updated": true})
}
func (s *Server) listIdentities(c *gin.Context) {
	rows, e := s.DB.QueryContext(c, `SELECT id,user_id,datasource_id,tenant_id,employee_id,department_id,warehouse_ids,external_tenant_id FROM novex_identities ORDER BY user_id,datasource_id`)
	if e != nil {
		fail(c, 500, "读取失败")
		return
	}
	defer rows.Close()
	out := []Identity{}
	for rows.Next() {
		var i Identity
		var j []byte
		if e = rows.Scan(&i.ID, &i.UserID, &i.DatasourceID, &i.TenantID, &i.EmployeeID, &i.DepartmentID, &j, &i.ExternalTenantID); e != nil {
			fail(c, 500, "读取失败")
			return
		}
		_ = json.Unmarshal(j, &i.WarehouseIDs)
		out = append(out, i)
	}
	ok(c, out)
}
func (s *Server) saveIdentity(c *gin.Context) {
	if !governance(actor(c)) {
		fail(c, 403, "身份映射需要授权治理角色")
		return
	}
	var i Identity
	if !bind(c, &i) {
		return
	}
	u, e := s.loadUser(c, i.UserID)
	if e != nil || u.TenantID != i.TenantID {
		fail(c, 400, "外部身份租户必须与账号租户一致")
		return
	}
	if _, e = s.datasource(c, i.DatasourceID); e != nil {
		fail(c, 400, "数据源不存在")
		return
	}
	if i.ExternalTenantID == "" {
		i.ExternalTenantID = i.TenantID
	}
	i.ID = id()
	j, _ := json.Marshal(i.WarehouseIDs)
	if string(j) == "null" {
		j = []byte("[]")
	}
	e = s.DB.QueryRowContext(c, `INSERT INTO novex_identities(id,user_id,datasource_id,tenant_id,employee_id,department_id,warehouse_ids,external_tenant_id) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8) ON CONFLICT(user_id,datasource_id) DO UPDATE SET tenant_id=excluded.tenant_id,employee_id=excluded.employee_id,department_id=excluded.department_id,warehouse_ids=excluded.warehouse_ids,external_tenant_id=excluded.external_tenant_id RETURNING id`, i.ID, i.UserID, i.DatasourceID, i.TenantID, i.EmployeeID, i.DepartmentID, string(j), i.ExternalTenantID).Scan(&i.ID)
	if e != nil {
		fail(c, 400, "保存身份映射失败")
		return
	}
	s.audit(c, actor(c).ID, "identity.save", i.ID, true, "trusted external actor mapping updated")
	ok(c, i)
}
func (s *Server) listGrants(c *gin.Context) {
	rows, e := s.DB.QueryContext(c, `SELECT id,role_id,resource_id,scope,fields FROM novex_grants ORDER BY role_id,resource_id`)
	if e != nil {
		fail(c, 500, "读取失败")
		return
	}
	defer rows.Close()
	out := []Grant{}
	for rows.Next() {
		var g Grant
		var j []byte
		_ = rows.Scan(&g.ID, &g.RoleID, &g.ResourceID, &g.Scope, &j)
		_ = json.Unmarshal(j, &g.Fields)
		out = append(out, g)
	}
	ok(c, out)
}
func (s *Server) saveGrant(c *gin.Context) {
	if !governance(actor(c)) {
		fail(c, 403, "权限配置需要授权治理角色")
		return
	}
	var g Grant
	if !bind(c, &g) {
		return
	}
	r, e := s.resource(c, g.ResourceID)
	if e != nil || !validScope(g.Scope) || len(g.Fields) == 0 {
		fail(c, 400, "资源、scope与字段授权不合法")
		return
	}
	if (g.Scope == "self" && r.OwnerColumn == "") || (g.Scope == "department" && r.DepartmentColumn == "") || (g.Scope == "warehouse" && r.WarehouseColumn == "") {
		fail(c, 400, "该资源缺少相应范围映射字段")
		return
	}
	for f, m := range g.Fields {
		if !contains(r.Columns, f) || (m != "plain" && m != "masked") {
			fail(c, 400, "字段授权必须来自注册字段且模式为plain/masked")
			return
		}
	}
	g.ID = id()
	j, _ := json.Marshal(g.Fields)
	_, e = s.DB.ExecContext(c, `INSERT INTO novex_grants(id,role_id,resource_id,scope,fields) VALUES($1,$2,$3,$4,$5::jsonb)`, g.ID, g.RoleID, r.ID, g.Scope, string(j))
	if e != nil {
		fail(c, 400, "保存授权失败")
		return
	}
	s.audit(c, actor(c).ID, "grant.create", r.ID, true, "row and field grant created")
	ok(c, g)
}
func (s *Server) deleteGrant(c *gin.Context) {
	if !governance(actor(c)) {
		fail(c, 403, "权限配置需要授权治理角色")
		return
	}
	_, e := s.DB.ExecContext(c, `DELETE FROM novex_grants WHERE id=$1`, c.Param("id"))
	if e != nil {
		fail(c, 500, "删除失败")
		return
	}
	s.audit(c, actor(c).ID, "grant.delete", c.Param("id"), true, "grant revoked")
	ok(c, gin.H{"deleted": true})
}
func (s *Server) listAudit(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, e := s.DB.QueryContext(c, `SELECT id,user_id,action,resource_id,allowed,detail,created_at FROM novex_audit ORDER BY created_at DESC LIMIT $1`, limit)
	if e != nil {
		fail(c, 500, "读取失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, action, resource, detail string
		var uid int64
		var allowed bool
		var created any
		_ = rows.Scan(&id, &uid, &action, &resource, &allowed, &detail, &created)
		out = append(out, map[string]any{"id": id, "userId": uid, "action": action, "resourceId": resource, "allowed": allowed, "detail": detail, "createdAt": created})
	}
	ok(c, out)
}

// enableProfile attaches business metadata to an existing shared system identity.
// It does not create credentials or reinterpret the existing system department.
func (s *Server) enableProfile(c *gin.Context) {
	if !governance(actor(c)) {
		fail(c, 403, "开通业务档案需要授权治理角色")
		return
	}
	uid, e := numericID(c.Param("id"))
	if e != nil {
		fail(c, 400, "无效账号")
		return
	}
	var input struct {
		TenantID     string `json:"tenantId"`
		DepartmentID string `json:"departmentId"`
	}
	if !bind(c, &input) {
		return
	}
	input.TenantID = strings.TrimSpace(input.TenantID)
	input.DepartmentID = strings.TrimSpace(input.DepartmentID)
	if input.TenantID == "" || len(input.TenantID) > 128 || len(input.DepartmentID) > 128 {
		fail(c, 400, "必须明确指定业务租户，租户和业务部门最多128字节")
		return
	}
	tx, e := s.DB.BeginTx(c, nil)
	if e != nil {
		fail(c, 500, "保存业务档案失败")
		return
	}
	defer tx.Rollback()
	var existingID int64
	if e = tx.QueryRowContext(c, `SELECT id FROM sys_user WHERE id=$1 FOR UPDATE`, uid).Scan(&existingID); e != nil {
		fail(c, 404, "系统账号不存在")
		return
	}
	var oldTenant string
	e = tx.QueryRowContext(c, `SELECT tenant_id FROM novex_users WHERE user_id=$1 FOR UPDATE`, uid).Scan(&oldTenant)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		fail(c, 500, "读取业务档案失败")
		return
	}
	if oldTenant != "" && oldTenant != input.TenantID {
		var bound bool
		e = tx.QueryRowContext(c, `SELECT EXISTS(SELECT 1 FROM novex_identities WHERE user_id=$1) OR EXISTS(SELECT 1 FROM novex_documents WHERE owner_id=$1) OR EXISTS(SELECT 1 FROM novex_conversations WHERE user_id=$1)`, uid).Scan(&bound)
		if e != nil {
			fail(c, 500, "检查业务引用失败")
			return
		}
		if bound {
			fail(c, 409, "该账号已有外部身份、文档或会话，不能直接变更租户；请先完成业务迁移")
			return
		}
	}
	_, e = tx.ExecContext(c, `INSERT INTO novex_users(user_id,tenant_id,department_id) VALUES($1,$2,$3) ON CONFLICT(user_id) DO UPDATE SET tenant_id=excluded.tenant_id,department_id=excluded.department_id`, uid, input.TenantID, input.DepartmentID)
	if e != nil || tx.Commit() != nil {
		fail(c, 500, "保存业务档案失败")
		return
	}
	u, e := s.loadSystemUser(c, uid)
	if e != nil {
		fail(c, 500, "读取账号失败")
		return
	}
	s.audit(c, actor(c).ID, "user.profile", strconv.FormatInt(uid, 10), true, "explicit business profile configured for shared system identity")
	ok(c, u)
}
