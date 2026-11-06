package novex

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAllManagementRoutesHaveRegisteredMenuPermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	(&Server{}).Register(router)
	permissions := map[string]string{}
	for _, route := range ManagementRoutePermissions() {
		key := route.Method + " " + route.Path
		if route.Permission == "" {
			t.Fatalf("management route has no permission: %s", key)
		}
		if _, exists := permissions[key]; exists {
			t.Fatalf("duplicate management rule: %s", key)
		}
		permissions[key] = route.Permission
	}
	seen := map[string]bool{}
	for _, route := range router.Routes() {
		if !strings.HasPrefix(route.Path, "/api/novex/admin/") {
			continue
		}
		key := route.Method + " " + route.Path
		if permissions[key] == "" {
			t.Fatalf("management endpoint missing shared RBAC rule: %s", key)
		}
		seen[key] = true
	}
	for key := range permissions {
		if !seen[key] {
			t.Fatalf("permission references an unregistered route: %s", key)
		}
	}
	registered := map[string]bool{"novex:console:view": true}
	for _, menu := range novexMenuPermissions {
		if registered[menu.Permission] {
			t.Fatalf("duplicate menu permission: %s", menu.Permission)
		}
		registered[menu.Permission] = true
	}
	for key, permission := range permissions {
		if strings.HasPrefix(permission, "novex:") && !registered[permission] {
			t.Fatalf("%s permission cannot be configured in sys_menu: %s", key, permission)
		}
	}
}

func TestMenuIntegrationKeepsAdditionalAdminAndGovernanceBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Run("governance_role_without_admin_does_not_unlock_management", func(t *testing.T) {
		s := &Server{}
		router := gin.New()
		router.Use(func(c *gin.Context) { c.Set("novex.user", &User{Roles: []Role{{Code: "novex_governance"}}}) })
		router.GET("/admin", s.admin, func(c *gin.Context) { c.Status(204) })
		res := httptest.NewRecorder()
		router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/admin", nil))
		if res.Code != 403 {
			t.Fatalf("expected 403, got %d", res.Code)
		}
	})
	t.Run("plain_admin_cannot_enable_business_profile", func(t *testing.T) {
		s := &Server{}
		router := gin.New()
		router.Use(func(c *gin.Context) { c.Set("novex.user", &User{IsAdmin: true, Roles: []Role{{Code: "admin"}}}) })
		router.PUT("/users/:id/profile", s.enableProfile)
		res := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/users/123/profile", strings.NewReader(`{"tenantId":"guessed-tenant","departmentId":"sales"}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(res, req)
		if res.Code != 403 {
			t.Fatalf("expected governance rejection before DB access, got %d", res.Code)
		}
	})
}

func TestSidebarPagesDoNotImplicitlyGrantManagementActions(t *testing.T) {
	if len(novexMenuPages) != 9 {
		t.Fatalf("expected 9 distinct sidebar pages, got %d", len(novexMenuPages))
	}
	pages := map[int64]menuPage{}
	paths := map[string]bool{}
	names := map[string]bool{}
	for _, page := range novexMenuPages {
		if _, exists := pages[page.ID]; exists || paths[page.Path] || names[page.Name] {
			t.Fatalf("duplicate page identity: %#v", page)
		}
		if !strings.HasSuffix(page.Component, "/index") || page.Component == "novex/index" {
			t.Fatalf("page still uses legacy all-in-one component: %s", page.Component)
		}
		if page.ID != 93010 && page.Permission != "" {
			t.Fatalf("inherited navigation must not grant a new API permission: %s", page.Permission)
		}
		pages[page.ID] = page
		paths[page.Path] = true
		names[page.Name] = true
	}
	if pages[93010].Path != "/novex/console" || pages[93010].Name != "NovexConsole" || pages[93010].Component != "novex/overview/index" {
		t.Fatal("existing console URL/name compatibility broken")
	}
	for _, permission := range novexMenuPermissions {
		if _, exists := pages[permission.ParentID]; !exists || permission.ParentID == 93010 {
			t.Fatalf("operation %s not assigned to its dedicated sidebar page", permission.Permission)
		}
	}
}
