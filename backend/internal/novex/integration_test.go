package novex

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"voc-go-backend/internal/infrastructure/db"
)

// Opt in after fixtures and seed: NOVEX_INTEGRATION=1 go test ./internal/novex -run Integration.
// Uses real read-only external DB connections; changes only metadata-only novex_audit.
func TestIntegrationSeededPermissionMatrix(t *testing.T) {
	if os.Getenv("NOVEX_INTEGRATION") != "1" {
		t.Skip("requires seeded local PostgreSQL demo")
	}
	pg, e := db.NewPostgres(db.LoadConfigFromEnv())
	if e != nil {
		t.Fatal(e)
	}
	defer pg.Close()
	key, e := loadKey()
	if e != nil {
		t.Fatal(e)
	}
	s := &Server{DB: pg, key: key}
	ctx := context.Background()
	query := func(uid int64, q Query) (map[string]any, error) {
		u, e := s.loadUser(ctx, uid)
		if e != nil {
			t.Fatal(e)
		}
		return s.Query(ctx, u, q)
	}
	rows := func(v map[string]any) []map[string]any { return v["rows"].([]map[string]any) }
	t.Run("employee_self_and_tenant", func(t *testing.T) {
		v, e := query(9201, Query{ResourceID: "payroll"})
		if e != nil {
			t.Fatal(e)
		}
		r := rows(v)
		if len(r) != 1 || r[0]["employee_id"] != "E001" || stringValue(r[0]["total"]) != "9000.00" {
			t.Fatalf("unexpected A payroll: %#v", r)
		}
	})
	t.Run("row_field_grants_not_cross_joined", func(t *testing.T) {
		v, e := query(9202, Query{ResourceID: "payroll"})
		if e != nil {
			t.Fatal(e)
		}
		r := rows(v)
		if len(r) != 3 {
			t.Fatalf("expected sales 3 records, got %d", len(r))
		}
		for _, row := range r {
			_, bonus := row["bonus"]
			if row["employee_id"] == "E002" && !bonus {
				t.Fatal("self bonus missing")
			}
			if row["employee_id"] != "E002" && bonus {
				t.Fatal("colleague bonus leaked")
			}
		}
	})
	t.Run("hr_mask_and_cross_tenant", func(t *testing.T) {
		v, e := query(9203, Query{ResourceID: "payroll"})
		if e != nil {
			t.Fatal(e)
		}
		r := rows(v)
		if len(r) != 5 {
			t.Fatalf("expected own tenant 5, got %d", len(r))
		}
		for _, row := range r {
			if row["bank_account"] != "***" {
				t.Fatal("bank account not masked")
			}
			if stringValue(row["total"]) == "66000.00" {
				t.Fatal("cross-tenant salary leaked")
			}
		}
	})
	t.Run("same_employee_id_other_tenant", func(t *testing.T) {
		v, e := query(9206, Query{ResourceID: "payroll"})
		if e != nil {
			t.Fatal(e)
		}
		r := rows(v)
		if len(r) != 1 || stringValue(r[0]["total"]) != "66000.00" {
			t.Fatal("external tenant mapping failed")
		}
	})
	t.Run("hidden_field_selection_and_predicates", func(t *testing.T) {
		for _, q := range []Query{{ResourceID: "products", Fields: []string{"cost_price"}}, {ResourceID: "products", Filters: []Filter{{Field: "cost_price", Op: "gte", Value: 0}}}, {ResourceID: "products", Sort: &Sort{Field: "cost_price", Direction: "asc"}}, {ResourceID: "products", Fields: []string{"sale_price; DROP TABLE x"}}} {
			if _, e := query(9201, q); e == nil {
				t.Fatalf("hidden/injected field accepted: %#v", q)
			}
		}
	})
	t.Run("warehouse_mapping", func(t *testing.T) {
		for uid, warehouse := range map[int64]string{9202: "W1", 9205: "W2"} {
			v, e := query(uid, Query{ResourceID: "inventory"})
			if e != nil {
				t.Fatal(e)
			}
			for _, row := range rows(v) {
				if row["warehouse_id"] != warehouse {
					t.Fatal("warehouse scope leaked")
				}
			}
		}
	})

	t.Run("wms_warehouses_and_movement_ledger", func(t *testing.T) {
		for _, test := range []struct {
			uid               int64
			warehouse, prefix string
			count             int
		}{{9202, "W1", "MOV-SH-", 6}, {9205, "W2", "MOV-SZ-", 4}, {9206, "W1", "MOV-GX-", 2}} {
			catalog, e := query(test.uid, Query{ResourceID: "warehouses"})
			if e != nil {
				t.Fatal(e)
			}
			catalogRows := rows(catalog)
			if len(catalogRows) != 1 || catalogRows[0]["warehouse_id"] != test.warehouse {
				t.Fatalf("warehouse catalog scope failed for %d", test.uid)
			}
			ledger, e := query(test.uid, Query{ResourceID: "stock_movements", Sort: &Sort{Field: "occurred_at", Direction: "desc"}, Limit: 100})
			if e != nil {
				t.Fatal(e)
			}
			ledgerRows := rows(ledger)
			if len(ledgerRows) != test.count {
				t.Fatalf("user %d expected %d movements, got %d", test.uid, test.count, len(ledgerRows))
			}
			balances := map[string]int{}
			for _, row := range ledgerRows {
				if row["warehouse_id"] != test.warehouse || !strings.HasPrefix(stringValue(row["movement_id"]), test.prefix) {
					t.Fatalf("cross-warehouse/tenant movement disclosed for %d", test.uid)
				}
				quantity, e := strconv.Atoi(stringValue(row["quantity"]))
				if e != nil {
					t.Fatal(e)
				}
				if row["movement_type"] == "outbound" {
					quantity = -quantity
				}
				balances[stringValue(row["sku"])] += quantity
				if row["document_no"] == "" || row["occurred_at"] == nil {
					t.Fatal("movement source reference missing")
				}
			}
			stock, e := query(test.uid, Query{ResourceID: "inventory"})
			if e != nil {
				t.Fatal(e)
			}
			for _, row := range rows(stock) {
				count, e := strconv.Atoi(stringValue(row["stock"]))
				if e != nil {
					t.Fatal(e)
				}
				if balances[stringValue(row["sku"])] != count {
					t.Fatalf("synthetic ledger does not reconcile for %d SKU %v", test.uid, row["sku"])
				}
			}
		}
		if _, e := query(9201, Query{ResourceID: "stock_movements"}); e == nil {
			t.Fatal("employee without warehouse grant read movement ledger")
		}
	})
	t.Run("platform_admin_is_not_business_reader", func(t *testing.T) {
		var ordinaryAdmin int64
		e := pg.QueryRowContext(ctx, `SELECT u.id FROM sys_user u JOIN novex_users n ON n.user_id=u.id WHERE u.status=1 AND EXISTS(SELECT 1 FROM sys_user_role ur JOIN sys_role r ON r.id=ur.role_id WHERE ur.user_id=u.id AND r.code='admin') AND NOT EXISTS(SELECT 1 FROM sys_user_role ur JOIN sys_role r ON r.id=ur.role_id WHERE ur.user_id=u.id AND r.code='novex_super_admin') ORDER BY CASE WHEN u.username='scale_admin' THEN 0 ELSE 1 END,u.id LIMIT 1`).Scan(&ordinaryAdmin)
		if errors.Is(e, sql.ErrNoRows) {
			t.Skip("requires a platform admin without explicit global data access")
		}
		if e != nil {
			t.Fatal(e)
		}
		if _, e := query(ordinaryAdmin, Query{ResourceID: "payroll"}); e == nil {
			t.Fatal("platform admin bypassed business grants")
		}
	})
	t.Run("explicit_global_reader_across_tenants_without_identity", func(t *testing.T) {
		var uid int64
		if e := pg.QueryRowContext(ctx, `SELECT id FROM sys_user WHERE username='admin'`).Scan(&uid); e != nil {
			t.Fatal(e)
		}
		u, e := s.loadUser(ctx, uid)
		if e != nil {
			t.Fatal(e)
		}
		if !globalDataAccess(u) {
			t.Skip("admin has not explicitly been granted novex_super_admin")
		}
		available, e := s.visible(ctx, u)
		if e != nil {
			t.Fatal(e)
		}
		registered, e := s.resources(ctx)
		if e != nil || len(available) != len(registered) {
			t.Fatal("global reader cannot discover all registered resources")
		}
		all, e := query(uid, Query{ResourceID: "payroll", Limit: 100})
		if e != nil {
			t.Fatal(e)
		}
		tenants := map[string]bool{}
		for _, row := range rows(all) {
			tenants[stringValue(row["tenant_id"])] = true
			if row["bank_account"] == "***" || row["bank_account"] == nil {
				t.Fatal("explicit global reader did not receive registered raw bank field")
			}
		}
		if !tenants["acme"] || !tenants["globex"] {
			t.Fatal("global query was incorrectly restricted to one tenant")
		}
		filtered, e := query(uid, Query{ResourceID: "payroll", Fields: []string{"tenant_id", "employee_id", "total"}, Filters: []Filter{{Field: "tenant_id", Op: "eq", Value: "globex"}}})
		if e != nil || len(rows(filtered)) != 1 || stringValue(rows(filtered)[0]["total"]) != "66000.00" {
			t.Fatal("global tenant discriminator cannot select same-ID records correctly")
		}
		injected, e := query(uid, Query{ResourceID: "payroll", Filters: []Filter{{Field: "tenant_id", Op: "eq", Value: "globex' OR TRUE--"}}})
		if e != nil || len(rows(injected)) != 0 {
			t.Fatal("global filter values were not safely parameterized")
		}
		for _, q := range []Query{{ResourceID: "payroll", Fields: []string{"unregistered_secret"}}, {ResourceID: "payroll", Filters: []Filter{{Field: "unregistered_secret", Op: "eq", Value: "x"}}}, {ResourceID: "payroll", Sort: &Sort{Field: "unregistered_secret", Direction: "asc"}}} {
			if _, e := query(uid, q); e == nil {
				t.Fatal("global role bypassed registered column whitelist")
			}
		}
		limited, e := query(uid, Query{ResourceID: "payroll", Limit: 1})
		if e != nil || len(rows(limited)) != 1 || limited["hasMore"] != true {
			t.Fatal("global role bypassed finite result page limits")
		}
	})
	t.Run("knowledge_acl_filters_content_and_metadata", func(t *testing.T) {
		u, e := s.loadUser(ctx, 9201)
		if e != nil {
			t.Fatal(e)
		}
		docs, e := s.documents(ctx, u, true)
		if e != nil {
			t.Fatal(e)
		}
		for _, d := range docs {
			if d.ID == "demo-hr-private" || d.ID == "demo-other-tenant" {
				t.Fatal("knowledge ACL leaked private document")
			}
		}
		if _, e = s.document(ctx, u, "demo-hr-private"); e == nil {
			t.Fatal("document direct read bypassed ACL")
		}
	})
}
