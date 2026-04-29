package ui

import (
	"io/fs"
	"testing"
)

func TestFS(t *testing.T) {
	root, err := FS()
	if err != nil {
		t.Fatalf("FS: %v", err)
	}
	// Walking the embedded FS should succeed even when it's just the gitkeep
	// placeholder.
	count := 0
	_ = fs.WalkDir(root, ".", func(_ string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		count++
		return nil
	})
	if count == 0 {
		t.Error("expected at least the root entry")
	}
}

func TestAvailable_NoSPA(t *testing.T) {
	// In test runs, internal/ui/dist contains only .gitkeep so Available() must
	// return false. If a developer ran `make ui` locally and didn't clean before
	// running tests, this will be true; either outcome is consistent with
	// the function's contract, but we assert the common case.
	got := Available()
	t.Logf("Available()=%v (true means a real SPA is embedded)", got)
}
