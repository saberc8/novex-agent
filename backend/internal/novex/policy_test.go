package novex

import (
	"strings"
	"testing"
)

func TestGrantsNeverCombineRowFromOneWithFieldsFromAnother(t *testing.T) {
	r := Resource{OwnerColumn: "employee_id", DepartmentColumn: "department_id"}
	i := Identity{EmployeeID: "E002", DepartmentID: "sales"}
	gs := []Grant{{Scope: "self", Fields: map[string]string{"base": "plain", "bonus": "plain"}}, {Scope: "department", Fields: map[string]string{"base": "plain"}}}
	colleague := map[string]any{"employee_id": "E001", "department_id": "sales"}
	if got := fieldMode(gs, r, i, colleague, "bonus"); got != "" {
		t.Fatalf("cross-grant salary disclosure: %s", got)
	}
	if got := fieldMode(gs, r, i, colleague, "base"); got != "plain" {
		t.Fatalf("expected base allowed: %s", got)
	}
	self := map[string]any{"employee_id": "E002", "department_id": "sales"}
	if got := fieldMode(gs, r, i, self, "bonus"); got != "plain" {
		t.Fatal("self grant should apply")
	}
	otherDept := map[string]any{"employee_id": "E005", "department_id": "rd"}
	if fieldMode(gs, r, i, otherDept, "base") != "" {
		t.Fatal("cross-department disclosure")
	}
}
func TestFilterAndSortCannotProbePartiallyHiddenFields(t *testing.T) {
	gs := []Grant{{Scope: "self", Fields: map[string]string{"total": "plain", "bonus": "plain"}}, {Scope: "department", Fields: map[string]string{"total": "plain", "bonus": "masked"}}}
	if fieldPlainForEveryGrant(gs, "bonus") {
		t.Fatal("masked field usable in filters")
	}
	if !fieldPlainForEveryGrant(gs, "total") {
		t.Fatal("fully authorized field rejected")
	}
	if fieldPlainForEveryGrant(nil, "total") {
		t.Fatal("empty grants must deny")
	}
}
func TestIdentityScopeFailsClosedAndParameterizesEmployee(t *testing.T) {
	r := Resource{OwnerColumn: "employee_id", WarehouseColumn: "warehouse_id"}
	args := []any{}
	sql, _ := scopeSQL(Grant{Scope: "self"}, r, Identity{}, &args)
	if sql != "FALSE" {
		t.Fatal("missing identity must deny")
	}
	payload := "E001' OR TRUE--"
	sql, _ = scopeSQL(Grant{Scope: "self"}, r, Identity{EmployeeID: payload}, &args)
	if strings.Contains(sql, payload) || len(args) != 1 || args[0] != payload {
		t.Fatalf("unparameterized employee identity: %s", sql)
	}
	args = nil
	sql, _ = scopeSQL(Grant{Scope: "warehouse"}, r, Identity{}, &args)
	if sql != "FALSE" {
		t.Fatal("empty warehouse mapping must deny")
	}
}
func TestMaskedGrantCannotDowngradeOrWidenScope(t *testing.T) {
	r := Resource{OwnerColumn: "employee_id"}
	i := Identity{EmployeeID: "E1"}
	gs := []Grant{{Scope: "tenant", Fields: map[string]string{"bank": "masked"}}, {Scope: "self", Fields: map[string]string{"bank": "plain"}}}
	if fieldMode(gs, r, i, map[string]any{"employee_id": "E2"}, "bank") != "masked" {
		t.Fatal("other employee bank should stay masked")
	}
	if fieldMode(gs, r, i, map[string]any{"employee_id": "E1"}, "bank") != "plain" {
		t.Fatal("explicit self plain should win")
	}
}

func TestGlobalReadKeepsRegisteredFieldsAndTenantDiscriminator(t *testing.T) {
	r := Resource{ID: "registered", DatasourceID: "source", Columns: []string{"employee_id", "bank_account"}, TenantColumn: "tenant_id", OwnerColumn: "employee_id", DepartmentColumn: "private_department"}
	global := globalReadResource(r)
	if len(global.Columns) != 3 || !contains(global.Columns, "tenant_id") {
		t.Fatal("global results must expose the registered tenant discriminator")
	}
	if len(r.Columns) != 2 || contains(r.Columns, "tenant_id") {
		t.Fatal("global access must not mutate ordinary users' resource columns")
	}
	if contains(global.Columns, "private_department") {
		t.Fatal("mapping fields outside the declared business/tenant columns were exposed")
	}
	grant := globalReadGrant(global)
	for _, tenant := range []string{"SC001", "SC002"} {
		row := map[string]any{"tenant_id": tenant, "employee_id": "E001"}
		if fieldMode([]Grant{grant}, global, Identity{}, row, "bank_account") != "plain" {
			t.Fatal("explicit global grant should read registered data across tenants")
		}
		if fieldMode([]Grant{grant}, global, Identity{}, row, "unregistered_secret") != "" {
			t.Fatal("global grant must not make unregistered fields readable")
		}
	}
	if fieldPlainForEveryGrant([]Grant{grant}, "unregistered_secret") {
		t.Fatal("unregistered fields must remain unavailable for filtering and sorting")
	}
	if len(globalReadResource(global).Columns) != len(global.Columns) {
		t.Fatal("tenant discriminator duplicated")
	}
}

func TestGlobalResourceMetadataDoesNotInventEmployeeIdentity(t *testing.T) {
	r := Resource{ID: "resource", Key: "payroll", DatasourceID: "source", TenantColumn: "tenant_id", Columns: []string{"employee_id", "total"}}
	view := globalVisibleResource(r, Datasource{Name: "HR source"})
	actor := view["actor"].(map[string]any)
	if actor["accessMode"] != "global" || actor["identityRequired"] != false {
		t.Fatal("global authority must be explicit in tool resource metadata")
	}
	for _, field := range []string{"employeeId", "departmentId", "externalTenantId", "warehouseIds"} {
		if _, exists := actor[field]; exists {
			t.Fatalf("global reader received a fabricated business identity: %s", field)
		}
	}
	if view["datasourceId"] != "source" || view["datasourceName"] != "HR source" || view["tenantColumn"] != "tenant_id" || view["scopes"].([]string)[0] != "global" {
		t.Fatal("global resource metadata cannot distinguish source and tenant")
	}
}
func TestCredentialEncryptionAuthenticatesCiphertext(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	encrypted, err := encrypt(key, "private-password")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encrypted, "private-password") {
		t.Fatal("stored plaintext")
	}
	plain, err := decrypt(key, encrypted)
	if err != nil || plain != "private-password" {
		t.Fatal("round trip failed")
	}
	if _, err = decrypt([]byte(strings.Repeat("x", 32)), encrypted); err == nil {
		t.Fatal("wrong key accepted")
	}
	tampered := encrypted[:len(encrypted)/2] + "AA" + encrypted[len(encrypted)/2+2:]
	if _, err = decrypt(key, tampered); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
}
