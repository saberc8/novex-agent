package novex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"voc-go-backend/internal/infrastructure/db"
)

// Read-only seeded database verification. Workspaces use t.TempDir; no document,
// role, identity or policy rows are created/changed by this test.
func TestIntegrationExplicitGlobalKnowledgeAccess(t *testing.T) {
	if os.Getenv("NOVEX_INTEGRATION") != "1" {
		t.Skip("requires seeded local PostgreSQL demo")
	}
	pg, err := db.NewPostgres(db.LoadConfigFromEnv())
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	s := &Server{DB: pg, DataDir: t.TempDir()}
	ctx := context.Background()
	ordinaryAdmin := &User{ID: 1, TenantID: "acme", DepartmentID: "admin", IsAdmin: true, Roles: []Role{{Code: "admin"}}}
	global := *ordinaryAdmin
	global.Roles = append([]Role{{Code: "novex_super_admin"}}, ordinaryAdmin.Roles...)

	all, err := s.documents(ctx, &global, false)
	if err != nil {
		t.Fatal(err)
	}
	var total int
	if err = pg.QueryRowContext(ctx, `SELECT count(*) FROM novex_documents`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if len(all) != total {
		t.Fatalf("global document list returned %d of %d", len(all), total)
	}
	limited, err := s.documents(ctx, ordinaryAdmin, false)
	if err != nil {
		t.Fatal(err)
	}
	visible := map[string]bool{}
	for _, d := range limited {
		visible[d.ID] = true
	}
	for _, fixture := range []struct{ id, tenant, marker string }{
		{"demo-hr-private", "acme", "HR-PRIVATE-CANARY-7391"},
		{"demo-other-tenant", "globex", "GLOBEX-ONLY-5639"},
	} {
		t.Run(fixture.id, func(t *testing.T) {
			if visible[fixture.id] {
				t.Fatal("ordinary platform admin unexpectedly listed restricted document")
			}
			if _, err := s.document(ctx, ordinaryAdmin, fixture.id); err == nil {
				t.Fatal("ordinary platform admin read restricted document")
			}
			d, err := s.document(ctx, &global, fixture.id)
			if err != nil || d.TenantID != fixture.tenant || !strings.Contains(d.Markdown, fixture.marker) {
				t.Fatalf("global direct read lost tenant/content: %v", err)
			}
			found, err := s.searchDocuments(ctx, &global, fixture.marker, 1)
			if err != nil || len(found) != 1 || found[0]["documentId"] != fixture.id || found[0]["tenantId"] != fixture.tenant || found[0]["scope"] != d.Scope {
				t.Fatalf("global indexed search did not preserve tenant/scope: %#v (%v)", found, err)
			}
			found, err = s.searchDocuments(ctx, ordinaryAdmin, fixture.marker, 1)
			if err != nil || len(found) != 0 {
				t.Fatalf("ordinary admin indexed search disclosed restricted content: %#v (%v)", found, err)
			}
		})
	}
	t.Run("native_workspace_uses_the_same_authority_and_tenant_labels", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			user *User
			all  bool
		}{{"ordinary", ordinaryAdmin, false}, {"global", &global, true}} {
			workspace, err := s.workspace(ctx, tc.user, tc.name)
			if err != nil {
				t.Fatal(err)
			}
			defer cleanupWorkspace(workspace)
			index, err := os.ReadFile(filepath.Join(workspace, "INDEX.md"))
			if err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(filepath.Join(workspace, "knowledge", "demo-other-tenant.md"))
			if tc.all {
				if err != nil || !strings.Contains(string(content), "tenant=globex") || !strings.Contains(string(index), "explicit global read access") {
					t.Fatal("global native workspace missing document or tenant disambiguation")
				}
			} else if !os.IsNotExist(err) || strings.Contains(string(index), "demo-other-tenant") {
				t.Fatal("ordinary native workspace contains another tenant's document")
			}
		}
	})
	t.Run("spreadsheet_metadata_uses_the_same_read_gate", func(t *testing.T) {
		var id, tenant string
		err := pg.QueryRowContext(ctx, `SELECT id,tenant_id FROM novex_documents WHERE kind='spreadsheet' AND scope='self' AND owner_id<>1 ORDER BY created_at LIMIT 1`).Scan(&id, &tenant)
		if err != nil {
			t.Skip("no private spreadsheet fixture installed")
		}
		if _, err = s.document(ctx, ordinaryAdmin, id); err == nil {
			t.Fatal("ordinary platform admin read another user's private workbook")
		}
		d, err := s.document(ctx, &global, id)
		if err != nil || d.Kind != "spreadsheet" || d.TenantID != tenant || len(d.Sheets) == 0 {
			t.Fatalf("global workbook metadata unavailable: %v", err)
		}
	})
}
