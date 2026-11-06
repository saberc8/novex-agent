package novex

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	httpif "voc-go-backend/internal/interfaces/http"
)

// ManagementRoutePermissions joins Novex management to the existing sys_menu /
// sys_role_menu contract. Existing admin and governance checks remain an additional
// restriction; a menu permission alone never grants access to business records.
func ManagementRoutePermissions() []httpif.RoutePermission {
	return []httpif.RoutePermission{
		{Method: "GET", Path: "/api/novex/admin/overview", Permission: "novex:console:view"},
		{Method: "GET", Path: "/api/novex/admin/users", Permission: "system:user:list"},
		{Method: "POST", Path: "/api/novex/admin/users", Permission: "system:user:create"},
		{Method: "PUT", Path: "/api/novex/admin/users/:id/roles", Permission: "system:user:updateRole"},
		{Method: "PUT", Path: "/api/novex/admin/users/:id/profile", Permission: "novex:user:enable"},
		{Method: "GET", Path: "/api/novex/admin/roles", Permission: "system:role:list"},
		{Method: "POST", Path: "/api/novex/admin/roles", Permission: "system:role:create"},
		{Method: "GET", Path: "/api/novex/admin/datasources", Permission: "novex:datasource:list"},
		{Method: "POST", Path: "/api/novex/admin/datasources", Permission: "novex:datasource:manage"},
		{Method: "PUT", Path: "/api/novex/admin/datasources/:id", Permission: "novex:datasource:manage"},
		{Method: "POST", Path: "/api/novex/admin/datasources/test", Permission: "novex:datasource:manage"},
		{Method: "POST", Path: "/api/novex/admin/datasources/:id/introspect", Permission: "novex:datasource:manage"},
		{Method: "GET", Path: "/api/novex/admin/resources", Permission: "novex:resource:list"},
		{Method: "POST", Path: "/api/novex/admin/resources", Permission: "novex:resource:manage"},
		{Method: "PUT", Path: "/api/novex/admin/resources/:id", Permission: "novex:resource:manage"},
		{Method: "GET", Path: "/api/novex/admin/identities", Permission: "novex:identity:list"},
		{Method: "POST", Path: "/api/novex/admin/identities", Permission: "novex:identity:manage"},
		{Method: "PUT", Path: "/api/novex/admin/identities/:id", Permission: "novex:identity:manage"},
		{Method: "GET", Path: "/api/novex/admin/grants", Permission: "novex:grant:list"},
		{Method: "POST", Path: "/api/novex/admin/grants", Permission: "novex:grant:manage"},
		{Method: "DELETE", Path: "/api/novex/admin/grants/:id", Permission: "novex:grant:manage"},
		{Method: "POST", Path: "/api/novex/admin/query/preview", Permission: "novex:grant:preview"},
		{Method: "GET", Path: "/api/novex/admin/audit", Permission: "novex:audit:list"},
	}
}

type menuPermission struct {
	ID                int64
	ParentID          int64
	Title, Permission string
}

type menuPage struct {
	ID                                       int64
	Title, Path, Name, Component, Permission string
	Sort                                     int
}

// New pages carry navigation only. Existing operation permission codes remain on
// their original button IDs, so extending a legacy console role to the new pages
// does not silently grant datasource/identity/grant management capabilities.
var novexMenuPages = []menuPage{
	{93010, "工作概览", "/novex/console", "NovexConsole", "novex/overview/index", "novex:console:view", 1},
	{93101, "数据连接", "/novex/datasources", "NovexDatasources", "novex/datasources/index", "", 2},
	{93102, "业务资源", "/novex/resources", "NovexResources", "novex/resources/index", "", 3},
	{93103, "身份绑定", "/novex/identities", "NovexIdentities", "novex/identities/index", "", 4},
	{93104, "数据授权", "/novex/grants", "NovexGrants", "novex/grants/index", "", 5},
	{93105, "权限预览", "/novex/preview", "NovexPreview", "novex/preview/index", "", 6},
	{93106, "知识资料", "/novex/knowledge", "NovexKnowledge", "novex/knowledge/index", "", 7},
	{93107, "运行审计", "/novex/audit", "NovexAudit", "novex/audit/index", "", 8},
	{93108, "对话助手", "/novex/chat", "NovexChat", "novex/chat/index", "", 9},
}

var novexMenuPermissions = []menuPermission{
	{93011, 93101, "查看数据源", "novex:datasource:list"}, {93012, 93101, "配置数据源", "novex:datasource:manage"},
	{93013, 93102, "查看业务资源", "novex:resource:list"}, {93014, 93102, "注册业务资源", "novex:resource:manage"},
	{93015, 93103, "查看外部身份", "novex:identity:list"}, {93016, 93103, "配置外部身份", "novex:identity:manage"},
	{93017, 93104, "查看数据授权", "novex:grant:list"}, {93018, 93104, "配置数据授权", "novex:grant:manage"},
	{93019, 93105, "预览有效权限", "novex:grant:preview"}, {93020, 93103, "开通业务档案", "novex:user:enable"},
	{93021, 93107, "查看业务审计", "novex:audit:list"},
}

// Existing console structure is upgraded once, identified by its former component.
// All mutations and inherited page links commit atomically. Later application
// starts only insert absent defaults and never reset custom menu attributes or
// reinstate a non-admin role's deliberately removed page link.
func (s *Server) migrateMenus(ctx context.Context) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var oldComponent string
	e = tx.QueryRowContext(ctx, `SELECT COALESCE(component,'') FROM sys_menu WHERE id=93010 FOR UPDATE`).Scan(&oldComponent)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	legacyConsole := e == nil && oldComponent == "novex/index"
	_, e = tx.ExecContext(ctx, `INSERT INTO sys_menu(id,title,parent_id,type,path,name,component,redirect,icon,is_external,is_cache,is_hidden,permission,sort,status,create_user,create_time)
 VALUES(93000,'智能业务',0,1,'/novex','Novex','Layout','/novex/console','robot',false,false,false,NULL,-10,1,1,now()) ON CONFLICT DO NOTHING`)
	if e != nil {
		return fmt.Errorf("seed Novex directory: %w", e)
	}
	for _, page := range novexMenuPages {
		_, e = tx.ExecContext(ctx, `INSERT INTO sys_menu(id,title,parent_id,type,path,name,component,icon,is_external,is_cache,is_hidden,permission,sort,status,create_user,create_time)
 VALUES($1,$2,93000,2,$3,$4,$5,'robot',false,false,false,NULLIF($6,''),$7,1,1,now()) ON CONFLICT DO NOTHING`, page.ID, page.Title, page.Path, page.Name, page.Component, page.Permission, page.Sort)
		if e != nil {
			return fmt.Errorf("seed Novex page %s: %w", page.Name, e)
		}
	}
	for _, permission := range novexMenuPermissions {
		_, e = tx.ExecContext(ctx, `INSERT INTO sys_menu(id,title,parent_id,type,is_external,is_cache,is_hidden,permission,sort,status,create_user,create_time)
 VALUES($1,$2,$3,3,false,false,false,$4,$5,1,1,now()) ON CONFLICT DO NOTHING`, permission.ID, permission.Title, permission.ParentID, permission.Permission, permission.ID-93010)
		if e != nil {
			return e
		}
		if legacyConsole {
			// Move only an untouched legacy parent assignment; custom title, permission,
			// order, visibility and manually reassigned parent remain the user's choice.
			_, e = tx.ExecContext(ctx, `UPDATE sys_menu SET parent_id=$2 WHERE id=$1 AND parent_id=93010 AND type=3 AND permission=$3`, permission.ID, permission.ParentID, permission.Permission)
			if e != nil {
				return e
			}
		}
	}
	if legacyConsole {
		for _, page := range novexMenuPages {
			if page.ID == 93010 {
				continue
			}
			_, e = tx.ExecContext(ctx, `INSERT INTO sys_role_menu(role_id,menu_id) SELECT role_id,$1 FROM sys_role_menu WHERE menu_id=93010 ON CONFLICT DO NOTHING`, page.ID)
			if e != nil {
				return e
			}
		}
		// Changing the marker happens in this transaction, after inherited navigation
		// links are copied. A rollback leaves the old structure safe to retry.
		_, e = tx.ExecContext(ctx, `UPDATE sys_menu SET component='novex/overview/index',title=CASE WHEN title IN ('Novex 工作空间','业务工作台') THEN '工作概览' ELSE title END WHERE id=93010 AND component='novex/index'`)
		if e != nil {
			return e
		}
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO sys_role_menu(role_id,menu_id) SELECT r.id,m.id FROM sys_role r CROSS JOIN sys_menu m WHERE r.code='admin' AND m.id IN (93000,93010,93011,93012,93013,93014,93015,93016,93017,93018,93019,93020,93021,93101,93102,93103,93104,93105,93106,93107,93108) ON CONFLICT DO NOTHING`)
	if e != nil {
		return e
	}
	return tx.Commit()
}
