package novex

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"voc-go-backend/internal/infrastructure/db"
)

// Opt in with NOVEX_MENU_MIGRATION_TEST=1. Every write is to connection-local TEMP
// tables that shadow sys_menu/sys_role/sys_role_menu; no application rows change.
func temporaryMenuDB(t *testing.T) *sql.DB {
	t.Helper()
	if os.Getenv("NOVEX_MENU_MIGRATION_TEST") != "1" {
		t.Skip("opt-in PostgreSQL temporary-table migration test")
	}
	pg, e := db.NewPostgres(db.LoadConfigFromEnv())
	if e != nil {
		t.Fatal(e)
	}
	pg.SetMaxOpenConns(1)
	pg.SetMaxIdleConns(1)
	t.Cleanup(func() { pg.Close() })
	_, e = pg.Exec(`CREATE TEMP TABLE sys_menu (LIKE public.sys_menu INCLUDING DEFAULTS INCLUDING CONSTRAINTS INCLUDING INDEXES);
 CREATE TEMP TABLE sys_role(id bigint PRIMARY KEY,code text NOT NULL);
 CREATE TEMP TABLE sys_role_menu(role_id bigint NOT NULL,menu_id bigint NOT NULL,UNIQUE(role_id,menu_id));
 INSERT INTO sys_role VALUES(1,'admin'),(2,'legacy-console-user'),(3,'unrelated-role');`)
	if e != nil {
		t.Fatal(e)
	}
	return pg
}

func TestMenuMigrationPreservesRolesAndCustomizations(t *testing.T) {
	pg := temporaryMenuDB(t)
	ctx := context.Background()
	_, e := pg.Exec(`INSERT INTO sys_menu(id,title,parent_id,type,path,name,component,permission,create_user,create_time) VALUES
 (93000,'智能业务',0,1,'/novex','Novex','Layout',NULL,1,now()),
 (93010,'Novex 工作空间',93000,2,'/novex/console','NovexConsole','novex/index','novex:console:view',1,now());
 INSERT INTO sys_role_menu VALUES(2,93010),(2,93011),(3,93011);`)
	if e != nil {
		t.Fatal(e)
	}
	for _, button := range novexMenuPermissions {
		_, e = pg.Exec(`INSERT INTO sys_menu(id,title,parent_id,type,permission,create_user,create_time) VALUES($1,$2,93010,3,$3,1,now())`, button.ID, button.Title, button.Permission)
		if e != nil {
			t.Fatal(e)
		}
	}
	s := &Server{DB: pg}
	if e = s.migrateMenus(ctx); e != nil {
		t.Fatal(e)
	}
	var component, title string
	var pages int
	if e = pg.QueryRow(`SELECT component,title FROM sys_menu WHERE id=93010`).Scan(&component, &title); e != nil {
		t.Fatal(e)
	}
	if component != "novex/overview/index" || title != "工作概览" {
		t.Fatalf("legacy console not upgraded: %s / %s", component, title)
	}
	if e = pg.QueryRow(`SELECT count(*) FROM sys_menu WHERE parent_id=93000 AND type=2`).Scan(&pages); e != nil || pages != 9 {
		t.Fatalf("expected 9 pages, got %d (%v)", pages, e)
	}
	for _, button := range novexMenuPermissions {
		var parent int64
		var permission string
		e = pg.QueryRow(`SELECT parent_id,permission FROM sys_menu WHERE id=$1`, button.ID).Scan(&parent, &permission)
		if e != nil || parent != button.ParentID || permission != button.Permission {
			t.Fatalf("button %d moved incorrectly: %d / %s (%v)", button.ID, parent, permission, e)
		}
	}
	var inherited, unrelated, extraAction int
	_ = pg.QueryRow(`SELECT count(*) FROM sys_role_menu WHERE role_id=2 AND menu_id BETWEEN 93101 AND 93108`).Scan(&inherited)
	_ = pg.QueryRow(`SELECT count(*) FROM sys_role_menu WHERE role_id=3 AND menu_id BETWEEN 93101 AND 93108`).Scan(&unrelated)
	_ = pg.QueryRow(`SELECT count(*) FROM sys_role_menu WHERE role_id=2 AND menu_id=93012`).Scan(&extraAction)
	if inherited != 8 || unrelated != 0 || extraAction != 0 {
		t.Fatalf("role continuity changed operation authority: inherited=%d unrelated=%d extraAction=%d", inherited, unrelated, extraAction)
	}
	// Simulate administrator customization after upgrade and an explicit page revoke.
	_, e = pg.Exec(`UPDATE sys_menu SET title='自定义数据连接',sort=777,is_hidden=true WHERE id=93101;
 UPDATE sys_menu SET parent_id=93103 WHERE id=93011;
 DELETE FROM sys_role_menu WHERE role_id=2 AND menu_id=93101;`)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.migrateMenus(ctx); e != nil {
		t.Fatal(e)
	}
	var order int
	var hidden bool
	var parent int64
	var revoked int
	_ = pg.QueryRow(`SELECT title,sort,is_hidden FROM sys_menu WHERE id=93101`).Scan(&title, &order, &hidden)
	_ = pg.QueryRow(`SELECT parent_id FROM sys_menu WHERE id=93011`).Scan(&parent)
	_ = pg.QueryRow(`SELECT count(*) FROM sys_role_menu WHERE role_id=2 AND menu_id=93101`).Scan(&revoked)
	if title != "自定义数据连接" || order != 777 || !hidden || parent != 93103 || revoked != 0 {
		t.Fatalf("repeat startup overwrote customization: title=%s order=%d hidden=%v parent=%d revoked-link=%d", title, order, hidden, parent, revoked)
	}
}

func TestMenuMigrationFreshInstall(t *testing.T) {
	pg := temporaryMenuDB(t)
	s := &Server{DB: pg}
	if e := s.migrateMenus(context.Background()); e != nil {
		t.Fatal(e)
	}
	var pages, adminLinks int
	_ = pg.QueryRow(`SELECT count(*) FROM sys_menu WHERE type=2 AND parent_id=93000`).Scan(&pages)
	_ = pg.QueryRow(`SELECT count(*) FROM sys_role_menu WHERE role_id=1 AND menu_id BETWEEN 93101 AND 93108`).Scan(&adminLinks)
	if pages != 9 || adminLinks != 8 {
		t.Fatalf("fresh install incomplete: pages=%d admin-page-links=%d", pages, adminLinks)
	}
}
