package novex

import (
	"context"
	"strings"
	"testing"
)

func TestSeedRequiresExplicitPrivatePassword(t *testing.T) {
	for _, password := range []string{"", "short", strings.Repeat("x", 73)} {
		t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", password)
		if err := (&Server{}).Seed(context.Background()); err == nil || !strings.Contains(err.Error(), "BOOTSTRAP_ADMIN_PASSWORD") {
			t.Fatal("seed must reject missing or invalid credentials before any database access")
		}
	}
}
