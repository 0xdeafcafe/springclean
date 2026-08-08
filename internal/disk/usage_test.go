package disk

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// write puts `size` bytes of distinctive content at path. Random-ish content
// matters: a filesystem is free to deduplicate or compress identical bytes,
// which would muddy what the test is measuring.
func write(t *testing.T, path string, size int) {
	t.Helper()
	buf := make([]byte, size)
	for i := range buf {
		buf[i] = byte(i*7 + len(path))
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

func stat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func TestChargeCountsAllocatedNotLogicalSize(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tiny.txt")
	write(t, p, 9)

	u := NewClaims(false).Charge(p, stat(t, p))
	if u.Apparent != 9 {
		t.Errorf("Apparent = %d, want 9", u.Apparent)
	}
	// Nine bytes take a whole block, or none at all if the filesystem tucks
	// them inside the inode. Either way, the logical size is not the answer.
	if u.Real == 9 {
		t.Errorf("Real = %d, which is the logical size rather than what was allocated", u.Real)
	}
}

func TestChargeCountsHardLinksOnce(t *testing.T) {
	dir := t.TempDir()
	orig := filepath.Join(dir, "orig.bin")
	link := filepath.Join(dir, "link.bin")
	write(t, orig, 128*1024)
	if err := os.Link(orig, link); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}

	c := NewClaims(false)
	first := c.Charge(orig, stat(t, orig))
	second := c.Charge(link, stat(t, link))

	if first.Real == 0 {
		t.Fatal("the first name for a file should be charged for it")
	}
	if second.Real != 0 {
		t.Errorf("Real = %d for a hard link, want 0: the blocks are already counted", second.Real)
	}
	if second.Shared != first.Real {
		t.Errorf("Shared = %d, want the %d bytes it shares with the original", second.Shared, first.Real)
	}
	// It is still a real file with a real size, and that number is unchanged.
	if second.Apparent != first.Apparent {
		t.Errorf("Apparent = %d, want %d", second.Apparent, first.Apparent)
	}
}

// TestChargeCountsClonesOnce is the pnpm case: `cp -c` on APFS gives the copy
// its own inode while both files point at the same storage, and the filesystem
// charges each of them the full block count. Adding those up is how fifty
// checkouts of one lockfile come to look like fifty times the disk.
func TestChargeCountsClonesOnce(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("clonefile is a macOS thing")
	}
	dir := t.TempDir()
	orig := filepath.Join(dir, "orig.bin")
	clone := filepath.Join(dir, "clone.bin")
	write(t, orig, 4*1024*1024)
	if out, err := exec.Command("cp", "-c", orig, clone).CombinedOutput(); err != nil {
		t.Skipf("clone unsupported here: %v (%s)", err, out)
	}

	origInfo, cloneInfo := stat(t, orig), stat(t, clone)
	if os.SameFile(origInfo, cloneInfo) {
		t.Fatal("a clone should be its own inode, so this test isn't measuring hard links")
	}

	c := NewClaims(true)
	first := c.Charge(orig, origInfo)
	second := c.Charge(clone, cloneInfo)

	if first.Real == 0 {
		t.Fatal("the original should be charged for its blocks")
	}
	if second.Real != 0 {
		t.Errorf("Real = %d for a clone, want 0: it shares every block with the original", second.Real)
	}
	if second.Shared != first.Real {
		t.Errorf("Shared = %d, want %d", second.Shared, first.Real)
	}
}

// TestChargeKeepsRealCopiesApart guards the other direction: a plain copy has
// its own blocks and must be charged for them, or springclean would report
// duplicated data as free to keep.
func TestChargeKeepsRealCopiesApart(t *testing.T) {
	dir := t.TempDir()
	orig := filepath.Join(dir, "orig.bin")
	copied := filepath.Join(dir, "copy.bin")
	write(t, orig, 1024*1024)
	if out, err := exec.Command("cp", orig, copied).CombinedOutput(); err != nil {
		t.Fatalf("cp failed: %v (%s)", err, out)
	}

	c := NewClaims(true)
	first := c.Charge(orig, stat(t, orig))
	second := c.Charge(copied, stat(t, copied))

	if second.Real != first.Real {
		t.Errorf("Real = %d for a genuine copy, want the same %d as the original", second.Real, first.Real)
	}
	if second.Shared != 0 {
		t.Errorf("Shared = %d, want 0: nothing is shared", second.Shared)
	}
}

// TestSkipDedupeChargesEverything is the escape hatch: with extent tracking
// off, no file is opened and every copy is charged in full.
func TestSkipDedupeChargesEverything(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("clonefile is a macOS thing")
	}
	dir := t.TempDir()
	orig := filepath.Join(dir, "orig.bin")
	clone := filepath.Join(dir, "clone.bin")
	write(t, orig, 1024*1024)
	if out, err := exec.Command("cp", "-c", orig, clone).CombinedOutput(); err != nil {
		t.Skipf("clone unsupported here: %v (%s)", err, out)
	}

	c := NewClaims(false)
	first := c.Charge(orig, stat(t, orig))
	second := c.Charge(clone, stat(t, clone))
	if second.Real != first.Real {
		t.Errorf("Real = %d, want %d: without extent tracking a clone looks like a copy",
			second.Real, first.Real)
	}
	if c.Enabled() {
		t.Error("Enabled() should be false when extents aren't being read")
	}
}

func TestNilClaimsChargesAllocatedSize(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	write(t, p, 64*1024)

	var c *Claims
	u := c.Charge(p, stat(t, p))
	if u.Real < 64*1024 {
		t.Errorf("Real = %d, want at least the 64 KiB written", u.Real)
	}
	if c.Enabled() {
		t.Error("a nil Claims tracks nothing")
	}
}

func TestUsageAdd(t *testing.T) {
	u := Usage{Apparent: 1, Real: 2, Shared: 3}
	u.Add(Usage{Apparent: 10, Real: 20, Shared: 30})
	if u != (Usage{Apparent: 11, Real: 22, Shared: 33}) {
		t.Errorf("Add gave %+v", u)
	}
}
