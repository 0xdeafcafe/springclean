package trash

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteManyRemovesFilesAndTrees(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "big.bin")
	if err := os.WriteFile(file, make([]byte, 1024), 0o644); err != nil {
		t.Fatal(err)
	}
	tree := filepath.Join(dir, "node_modules", "pkg", "deep")
	if err := os.MkdirAll(tree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "index.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	modules := filepath.Join(dir, "node_modules")

	res := DeleteMany([]string{file, modules})

	if len(res.Trashed) != 2 {
		t.Errorf("deleted %d paths, want 2", len(res.Trashed))
	}
	if len(res.Failed) != 0 {
		t.Errorf("failures: %v", res.Failed)
	}
	if res.Method != "deleted" {
		t.Errorf("Method = %q, want deleted", res.Method)
	}
	for _, p := range []string{file, modules} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s is still there", p)
		}
	}
}

// TestDeleteManySkipsMissing keeps a report that has gone stale from reading
// as a pile of failures.
func TestDeleteManySkipsMissing(t *testing.T) {
	dir := t.TempDir()
	gone := filepath.Join(dir, "not-here")

	res := DeleteMany([]string{gone})
	if len(res.Skipped) != 1 {
		t.Errorf("Skipped = %v, want the missing path", res.Skipped)
	}
	if len(res.Failed) != 0 {
		t.Errorf("a path that is already gone is not a failure: %v", res.Failed)
	}
}

// TestDeleteManyFollowsNoSymlinks: deleting a link must not delete what it
// points at. RemoveAll unlinks the symlink itself, and this pins that down.
func TestDeleteManyFollowsNoSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "keep")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(target, "important.txt")
	if err := os.WriteFile(real, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	DeleteMany([]string{link})

	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Error("the symlink should be gone")
	}
	if _, err := os.Stat(real); err != nil {
		t.Errorf("deleting a symlink took its target with it: %v", err)
	}
}

func TestDeleteManyEmpty(t *testing.T) {
	res := DeleteMany(nil)
	if len(res.Trashed) != 0 || len(res.Failed) != 0 {
		t.Errorf("nothing in, nothing out: %+v", res)
	}
}
