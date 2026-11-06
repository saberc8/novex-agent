package novex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"net"
	"net/url"
	"strconv"
	"time"
)

func (s *Server) datasource(ctx context.Context, id string) (Datasource, error) {
	var d Datasource
	err := s.DB.QueryRowContext(ctx, `SELECT id,name,host,port,database_name,username,password_encrypted,ssl_mode,created_at,tenant_id FROM novex_datasources WHERE id=$1`, id).Scan(&d.ID, &d.Name, &d.Host, &d.Port, &d.Database, &d.Username, &d.encrypted, &d.SSLMode, &d.CreatedAt, &d.TenantID)
	d.HasPassword = d.encrypted != ""
	return d, err
}
func (s *Server) external(ctx context.Context, d Datasource) (*sql.DB, error) {
	password := d.Password
	var err error
	if password == "" && d.encrypted != "" {
		password, err = decrypt(s.key, d.encrypted)
		if err != nil {
			return nil, err
		}
	}
	u := &url.URL{Scheme: "postgres", User: url.UserPassword(d.Username, password), Host: net.JoinHostPort(d.Host, strconv.Itoa(d.Port)), Path: "/" + d.Database}
	q := url.Values{}
	q.Set("sslmode", d.SSLMode)
	q.Set("connect_timeout", "5")
	q.Set("application_name", "novex_readonly")
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(3)
	db.SetConnMaxLifetime(time.Minute)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("无法连接数据库，请检查地址、账号和SSL设置")
	}
	return db, nil
}
func validateDatasource(d *Datasource) error {
	if blank(d.Host) || blank(d.Database) || blank(d.Username) || blank(d.Name) {
		return fmt.Errorf("名称、地址、数据库、用户名必填")
	}
	if d.Port == 0 {
		d.Port = 5432
	}
	if d.Port < 1 || d.Port > 65535 {
		return fmt.Errorf("端口不合法")
	}
	if d.SSLMode == "" {
		d.SSLMode = "disable"
	}
	if d.SSLMode != "disable" && d.SSLMode != "require" && d.SSLMode != "verify-full" {
		return fmt.Errorf("不支持的SSL模式")
	}
	return nil
}
func (s *Server) checkDatasource(ctx context.Context, d Datasource) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 7*time.Second)
	defer cancel()
	db, err := s.external(ctx, d)
	if err != nil {
		return "", err
	}
	defer db.Close()
	var version string
	var unsafe bool
	err = db.QueryRowContext(ctx, `SELECT version(), EXISTS(SELECT 1 FROM pg_roles WHERE rolname=current_user AND (rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole)) OR EXISTS(SELECT 1 FROM information_schema.tables t WHERE t.table_schema NOT IN ('pg_catalog','information_schema') AND has_table_privilege(current_user,quote_ident(t.table_schema)||'.'||quote_ident(t.table_name),'INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER'))`).Scan(&version, &unsafe)
	if err != nil {
		return "", fmt.Errorf("无法验证数据库只读权限")
	}
	if unsafe {
		return "", fmt.Errorf("业务数据库必须使用只读账号；当前账号含写入或管理权限")
	}
	return version, nil
}
func (s *Server) listDatasources(c *gin.Context) {
	rows, e := s.DB.QueryContext(c, `SELECT id FROM novex_datasources ORDER BY created_at`)
	if e != nil {
		fail(c, 500, "读取失败")
		return
	}
	defer rows.Close()
	out := []Datasource{}
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		d, e := s.datasource(c, id)
		if e == nil {
			out = append(out, d)
		}
	}
	ok(c, out)
}
func (s *Server) saveDatasource(c *gin.Context) {
	var d Datasource
	if !bind(c, &d) {
		return
	}
	if e := validateDatasource(&d); e != nil {
		fail(c, 400, e.Error())
		return
	}
	d.ID = c.Param("id")
	if d.ID != "" {
		old, e := s.datasource(c, d.ID)
		if e != nil {
			fail(c, 404, "数据源不存在")
			return
		}
		d.encrypted = old.encrypted
	} else {
		d.ID = id()
	}
	if d.Password == "" && d.encrypted == "" {
		fail(c, 400, "密码必填")
		return
	}
	if _, e := s.checkDatasource(c, d); e != nil {
		fail(c, 400, e.Error())
		return
	}
	if d.Password != "" {
		var e error
		d.encrypted, e = encrypt(s.key, d.Password)
		if e != nil {
			fail(c, 500, "凭据加密失败")
			return
		}
	}
	_, e := s.DB.ExecContext(c, `INSERT INTO novex_datasources(id,name,host,port,database_name,username,password_encrypted,ssl_mode,tenant_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(id) DO UPDATE SET name=excluded.name,host=excluded.host,port=excluded.port,database_name=excluded.database_name,username=excluded.username,password_encrypted=excluded.password_encrypted,ssl_mode=excluded.ssl_mode,tenant_id=excluded.tenant_id,schema_snapshot='[]'`, d.ID, d.Name, d.Host, d.Port, d.Database, d.Username, d.encrypted, d.SSLMode, d.TenantID)
	if e != nil {
		fail(c, 500, "保存失败")
		return
	}
	d.Password = ""
	d.HasPassword = true
	s.audit(c, actor(c).ID, "datasource.save", d.ID, true, "connection configured; credential encrypted")
	ok(c, d)
}
func (s *Server) testDatasource(c *gin.Context) {
	var r Datasource
	if !bind(c, &r) {
		return
	}
	if r.ID != "" {
		saved, e := s.datasource(c, r.ID)
		if e != nil {
			fail(c, 404, "数据源不存在")
			return
		}
		if r.Name != "" {
			saved.Name = r.Name
		}
		if r.Host != "" {
			saved.Host = r.Host
		}
		if r.Port != 0 {
			saved.Port = r.Port
		}
		if r.Database != "" {
			saved.Database = r.Database
		}
		if r.Username != "" {
			saved.Username = r.Username
		}
		if r.Password != "" {
			saved.Password = r.Password
		}
		if r.SSLMode != "" {
			saved.SSLMode = r.SSLMode
		}
		r = saved
	}
	if r.Name == "" {
		r.Name = "connection test"
	}
	if e := validateDatasource(&r); e != nil {
		fail(c, 400, e.Error())
		return
	}
	v, e := s.checkDatasource(c, r)
	if e != nil {
		fail(c, 400, e.Error())
		return
	}
	ok(c, gin.H{"ok": true, "readOnly": true, "serverVersion": v})
}
func (s *Server) scanSchema(ctx context.Context, d Datasource) ([]Table, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	db, e := s.external(ctx, d)
	if e != nil {
		return nil, e
	}
	defer db.Close()
	rows, e := db.QueryContext(ctx, `SELECT c.table_schema,c.table_name,c.column_name,c.data_type,c.is_nullable='YES' FROM information_schema.columns c JOIN information_schema.tables t ON t.table_schema=c.table_schema AND t.table_name=c.table_name WHERE c.table_schema NOT IN ('pg_catalog','information_schema') AND t.table_type='BASE TABLE' AND has_table_privilege(current_user,quote_ident(c.table_schema)||'.'||quote_ident(c.table_name),'SELECT') ORDER BY c.table_schema,c.table_name,c.ordinal_position LIMIT 5000`)
	if e != nil {
		return nil, fmt.Errorf("扫描schema失败")
	}
	defer rows.Close()
	out := []Table{}
	index := map[string]int{}
	for rows.Next() {
		var schema, name string
		var col Column
		if e = rows.Scan(&schema, &name, &col.Name, &col.DataType, &col.Nullable); e != nil {
			return nil, e
		}
		key := schema + "." + name
		i, exists := index[key]
		if !exists {
			i = len(out)
			index[key] = i
			out = append(out, Table{Schema: schema, Name: name, Columns: []Column{}})
		}
		out[i].Columns = append(out[i].Columns, col)
	}
	return out, rows.Err()
}
func (s *Server) introspect(c *gin.Context) {
	d, e := s.datasource(c, c.Param("id"))
	if e != nil {
		fail(c, 404, "数据源不存在")
		return
	}
	tables, e := s.scanSchema(c, d)
	if e != nil {
		fail(c, 400, e.Error())
		return
	}
	j, _ := json.Marshal(tables)
	_, e = s.DB.ExecContext(c, `UPDATE novex_datasources SET schema_snapshot=$2::jsonb WHERE id=$1`, d.ID, string(j))
	if e != nil {
		fail(c, 500, "保存schema失败")
		return
	}
	s.audit(c, actor(c).ID, "schema.introspect", d.ID, true, fmt.Sprintf("%d tables", len(tables)))
	ok(c, gin.H{"tables": tables})
}
func (s *Server) resource(ctx context.Context, id string) (Resource, error) {
	var r Resource
	var data []byte
	e := s.DB.QueryRowContext(ctx, `SELECT config FROM novex_resources WHERE id=$1 OR resource_key=$1`, id).Scan(&data)
	if e == nil {
		e = json.Unmarshal(data, &r)
	}
	return r, e
}
func (s *Server) resources(ctx context.Context) ([]Resource, error) {
	rows, e := s.DB.QueryContext(ctx, `SELECT config FROM novex_resources ORDER BY name`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Resource{}
	for rows.Next() {
		var data []byte
		var r Resource
		if e = rows.Scan(&data); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(data, &r); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Server) listResources(c *gin.Context) {
	r, e := s.resources(c)
	if e != nil {
		fail(c, 500, "读取失败")
		return
	}
	ok(c, r)
}
func (s *Server) validateResource(ctx context.Context, r *Resource) error {
	if blank(r.Name) || blank(r.Key) || len(r.Columns) == 0 {
		return fmt.Errorf("名称、key、租户字段与业务字段必填")
	}
	var snapshot []byte
	if e := s.DB.QueryRowContext(ctx, `SELECT schema_snapshot FROM novex_datasources WHERE id=$1`, r.DatasourceID).Scan(&snapshot); e != nil {
		return fmt.Errorf("数据源不存在")
	}
	var tables []Table
	if e := json.Unmarshal(snapshot, &tables); e != nil {
		return e
	}
	allowed := map[string]bool{}
	for _, t := range tables {
		if t.Schema == r.Schema && t.Name == r.Table {
			for _, c := range t.Columns {
				allowed[c.Name] = true
			}
		}
	}
	if len(allowed) == 0 {
		return fmt.Errorf("请先扫描schema并选择已发现的表")
	}
	checks := append([]string{}, r.Columns...)
	if r.TenantColumn != "" {
		checks = append(checks, r.TenantColumn)
	} else {
		d, e := s.datasource(ctx, r.DatasourceID)
		if e != nil || d.TenantID == "" {
			return fmt.Errorf("省略租户字段仅允许绑定内部租户的数据源")
		}
	}
	for _, v := range []string{r.OwnerColumn, r.DepartmentColumn, r.WarehouseColumn} {
		if v != "" {
			checks = append(checks, v)
		}
	}
	seen := map[string]bool{}
	for _, col := range checks {
		if !allowed[col] {
			return fmt.Errorf("字段不在schema白名单：%s", col)
		}
	}
	for _, col := range r.Columns {
		if seen[col] {
			return fmt.Errorf("字段重复")
		}
		seen[col] = true
	}
	r.IdentityRequired = true
	return nil
}
func (s *Server) saveResource(c *gin.Context) {
	var r Resource
	if !bind(c, &r) {
		return
	}
	r.ID = c.Param("id")
	if r.ID == "" {
		r.ID = id()
	}
	if e := s.validateResource(c, &r); e != nil {
		fail(c, 400, e.Error())
		return
	}
	j, _ := json.Marshal(r)
	_, e := s.DB.ExecContext(c, `INSERT INTO novex_resources(id,resource_key,name,datasource_id,config) VALUES($1,$2,$3,$4,$5::jsonb) ON CONFLICT(id) DO UPDATE SET resource_key=excluded.resource_key,name=excluded.name,datasource_id=excluded.datasource_id,config=excluded.config`, r.ID, r.Key, r.Name, r.DatasourceID, string(j))
	if e != nil {
		fail(c, 400, "保存失败，检查key是否重复")
		return
	}
	s.audit(c, actor(c).ID, "resource.save", r.ID, true, "allowlisted resource configured")
	ok(c, r)
}
