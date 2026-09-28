package scan

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/0xdeafcafe/springclean/internal/domain"
)

// cloneTree copies src to dst with clonefile, the way a package manager
// installs a second checkout's dependencies: separate inodes, shared blocks,
// and a filesystem that charges both of them in full.
func cloneTree(t *testing.T, src, dst string) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("clonefile is a macOS thing")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cp", "-cR", src, dst).CombinedOutput(); err != nil {
		t.Skipf("clone unsupported here: %v (%s)", err, out)
	}
}

// TestClonedCachesAreCountedOnce is the pnpm problem. Every checkout gets its
// own node_modules, each one a clone of the same store, and the filesystem
// charges each the full block count. Summing those promises gigabytes back
// that deleting them would not give.
func TestClonedCachesAreCountedOnce(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "one", "node_modules")
	writeSized(t, filepath.Join(first, "pkg", "index.js"), 4*1024*1024)
	cloneTree(t, first, filepath.Join(root, "two", "node_modules"))

	caches := findByCategory(runScan(t, Config{Mode: domain.ModeRoot, Root: root}), domain.CatDevCache)
	if len(caches) != 2 {
		t.Fatalf("found %d dev caches, want both node_modules", len(caches))
	}

	var charged, free int
	for _, c := range caches {
		if c.Apparent < 4*1024*1024 {
			t.Errorf("%s: Apparent = %d, want the full size on paper", c.Path, c.Apparent)
		}
		if c.Size > 1024*1024 {
			charged++
			continue
		}
		free++
		if c.Shared < 4*1024*1024 {
			t.Errorf("%s: Shared = %d, want the ~4 MB it shares with the other copy", c.Path, c.Shared)
		}
		if !c.Duplicated() {
			t.Errorf("%s: should read as duplicated", c.Path)
		}
	}
	if charged != 1 || free != 1 {
		t.Errorf("%d copies charged and %d free, want exactly one of each", charged, free)
	}
}

// TestSkipDedupeCountsEveryCopy checks the opt-out still reports the old
// numbers, for anyone who would rather have the faster scan.
func TestSkipDedupeCountsEveryCopy(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "one", "node_modules")
	writeSized(t, filepath.Join(first, "pkg", "index.js"), 4*1024*1024)
	cloneTree(t, first, filepath.Join(root, "two", "node_modules"))

	caches := findByCategory(
		runScan(t, Config{Mode: domain.ModeRoot, Root: root, SkipDedupe: true}),
		domain.CatDevCache)
	if len(caches) != 2 {
		t.Fatalf("found %d dev caches, want 2", len(caches))
	}
	for _, c := range caches {
		if c.Size < 4*1024*1024 {
			t.Errorf("%s: Size = %d, want the full size with sharing ignored", c.Path, c.Size)
		}
	}
}

// TestHardLinkedFilesAreCountedOnce covers the same idea without clonefile:
// two names for one set of blocks are not two sets of blocks.
func TestHardLinkedFilesAreCountedOnce(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "one", "node_modules", "big.bin")
	writeSized(t, a, 3*1024*1024)
	b := filepath.Join(root, "two", "node_modules", "big.bin")
	if err := os.MkdirAll(filepath.Dir(b), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(a, b); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}

	caches := findByCategory(runScan(t, Config{Mode: domain.ModeRoot, Root: root}), domain.CatDevCache)
	if len(caches) != 2 {
		t.Fatalf("found %d dev caches, want 2", len(caches))
	}
	var total int64
	for _, c := range caches {
		total += c.Size
	}
	if total > 4*1024*1024 {
		t.Errorf("the two together are charged %d bytes, want roughly the 3 MB they share", total)
	}
}

// TestSizeIsAllocatedNotLogical guards the smaller half of the fix: a tree of
// tiny files takes more room than their bytes add up to, and one full of holes
// takes less.
func TestSizeIsAllocatedNotLogical(t *testing.T) {
	root := t.TempDir()
	cache := filepath.Join(root, "proj", "node_modules")
	for i := 0; i < 400; i++ {
		writeSized(t, filepath.Join(cache, "pkg", string(rune('a'+i%26))+itoaTest(i)+".js"), 10)
	}

	caches := findByCategory(runScan(t, Config{Mode: domain.ModeRoot, Root: root}), domain.CatDevCache)
	if len(caches) != 1 {
		t.Fatalf("found %d dev caches, want 1", len(caches))
	}
	c := caches[0]
	if c.Apparent != 4000 {
		t.Errorf("Apparent = %d, want the 4000 bytes written", c.Apparent)
	}
	if c.Size <= c.Apparent {
		t.Errorf("Size = %d, want more than the %d bytes of content: 400 files occupy 400 blocks",
			c.Size, c.Apparent)
	}
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
