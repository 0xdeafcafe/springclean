package disk

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

// ReadDir must agree with lstat on every entry, and see through a clone.
func TestReadDirMatchesLstatAndSeesClones(t *testing.T) {
	dir := t.TempDir()
	orig := filepath.Join(dir, "orig")
	if err := os.WriteFile(orig, make([]byte, 1<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cp", "-c", orig, filepath.Join(dir, "clone")).CombinedOutput(); err != nil {
		t.Skipf("no clonefile here: %s", out)
	}
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	os.Symlink("orig", filepath.Join(dir, "link"))

	for _, d := range []string{dir, "/usr/bin", os.Getenv("HOME")} {
		entries, err := ReadDir(d)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := os.ReadDir(d)
		if len(entries) != len(want) {
			t.Errorf("%s: %d entries, os.ReadDir has %d", d, len(entries), len(want))
		}
		for _, e := range entries {
			info, err := os.Lstat(filepath.Join(d, e.Name))
			if err != nil {
				t.Errorf("%s: bad name %q", d, e.Name)
				continue
			}
			st := info.Sys().(*syscall.Stat_t)
			if e.IsDir != info.IsDir() || e.Symlink != (info.Mode()&os.ModeSymlink != 0) ||
				e.Ino != st.Ino || e.Dev != st.Dev || !e.ModTime.Equal(info.ModTime()) {
				t.Errorf("%s/%s: type/ino/dev/mtime mismatch: %+v", d, e.Name, e)
			}
			if e.Regular && (e.Size != info.Size() || e.Alloc != st.Blocks*512 || e.Nlink != uint32(st.Nlink)) {
				t.Errorf("%s/%s: size %d/%d alloc %d/%d", d, e.Name, e.Size, info.Size(), e.Alloc, st.Blocks*512)
			}
		}
	}

	byName := map[string]Entry{}
	entries, _ := ReadDir(dir)
	for _, e := range entries {
		byName[e.Name] = e
	}
	a, b := byName["orig"], byName["clone"]
	t.Logf("orig %+v\nclone %+v", a, b)
	if !a.HasClone || a.CloneID != b.CloneID || a.Ino == b.Ino {
		t.Errorf("clones not detected: orig %+v, clone %+v", a, b)
	}
	c := NewClaims(true)
	if first, second := c.ChargeEntry("", a), c.ChargeEntry("", b); first.Real != a.Alloc || second.Real != 0 || second.Shared != b.Alloc {
		t.Errorf("clone charged twice: first %+v, second %+v", first, second)
	}
}
