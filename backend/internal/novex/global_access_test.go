package novex

import "testing"

func TestGlobalDataAccessRequiresExplicitRole(t *testing.T) {
	for _, user := range []*User{nil, {}, {Username: "admin", IsAdmin: true}, {GlobalDataAccess: true}, {Roles: []Role{{Code: "admin"}, {Code: "novex_governance"}}}} {
		if globalDataAccess(user) {
			t.Fatal("ordinary administration or a cached flag cannot grant global data access")
		}
	}
	if !globalDataAccess(&User{Roles: []Role{{Code: "novex_super_admin"}}}) {
		t.Fatal("explicit global data role must be recognized")
	}
}
