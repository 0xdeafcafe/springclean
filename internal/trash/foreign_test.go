package trash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNeedsAdmin(t *testing.T) {
	mine := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(mine, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if needsAdmin(mine) {
		t.Error("own file in own folder flagged as needing admin")
	}
	if !needsAdmin("/usr/bin/true") {
		t.Error("root-owned file not flagged")
	}
}

func TestAdminDeleteScriptQuotesPaths(t *testing.T) {
	got := adminDeleteScript([]string{`/Users/x/it's "odd"`, "/a b"})
	want := `do shell script "/bin/rm -rf -- '/Users/x/it'\\''s \"odd\"' '/a b'"`
	if !strings.HasPrefix(got, want) {
		t.Errorf("got  %s\nwant %s...", got, want)
	}
}
