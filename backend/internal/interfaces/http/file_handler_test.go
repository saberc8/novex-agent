package http

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeParentPathRejectsTraversal(t *testing.T) {
	for _, candidate := range []string{"../tmp", "/../../tmp", "/safe/../tmp", `safe\..\tmp`} {
		if _, err := normalizeParentPath(candidate); err == nil {
			t.Fatalf("normalizeParentPath(%q) accepted traversal", candidate)
		}
	}
}

func TestSafeLocalStoragePathStaysWithinRoot(t *testing.T) {
	root := t.TempDir()
	resolved, err := safeLocalStoragePath(root, "/avatars/user.png")
	if err != nil {
		t.Fatalf("safeLocalStoragePath returned error: %v", err)
	}
	rootWithSeparator := root + string(filepath.Separator)
	if resolved != root && !strings.HasPrefix(resolved, rootWithSeparator) {
		t.Fatalf("resolved path %q escaped root %q", resolved, root)
	}

	if _, err := safeLocalStoragePath(root, "/../../outside"); err == nil {
		t.Fatal("safeLocalStoragePath accepted an escaping path")
	}
}
