package scan

import (
	"path/filepath"
	"testing"

	"github.com/0xdeafcafe/springclean/internal/domain"
)

// The large-folder pass should land on the tightest folder holding the bulk,
// and stay quiet about folders whose size another finding already explains.
func TestLargeDirsReportTightestUnexplainedFolder(t *testing.T) {
	defer func(v int64) { largeDirThreshold = v }(largeDirThreshold)
	largeDirThreshold = 4 << 20

	root := t.TempDir()
	for _, n := range []string{"a", "b", "c"} {
		writeSized(t, filepath.Join(root, "models", "blobs", n), 2<<20)
	}
	writeSized(t, filepath.Join(root, "models", "small"), 1<<20)
	writeSized(t, filepath.Join(root, "app", "node_modules", "dep", "x"), 8<<20)

	got := map[string]bool{}
	for _, s := range findByCategory(runScan(t, Config{Mode: domain.ModeRoot, Root: root}), domain.CatLargeDir) {
		got[s.Path] = true
	}
	if !got[filepath.Join(root, "models", "blobs")] {
		t.Errorf("blobs not reported; got %v", got)
	}
	if got[filepath.Join(root, "models")] || got[root] {
		t.Errorf("reported a folder whose bulk is already listed; got %v", got)
	}
	if got[filepath.Join(root, "app")] {
		t.Errorf("app reported though node_modules already explains it; got %v", got)
	}
}
