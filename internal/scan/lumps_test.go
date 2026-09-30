package scan

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/0xdeafcafe/springclean/internal/disk"
)

func TestLumpCacheReusesOnlyUnchangedDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sizes.gob")
	e := disk.Entry{Ino: 42, ModTime: time.Now()}
	u := disk.Usage{Apparent: 10, Real: 8}

	c := openLumps(path)
	c.put("/x", e, u)
	if err := c.save(); err != nil {
		t.Fatal(err)
	}

	c = openLumps(path)
	if got, ok := c.get("/x", e); !ok || got != u {
		t.Errorf("unchanged dir missed the cache: %+v %v", got, ok)
	}
	touched := e
	touched.ModTime = e.ModTime.Add(time.Second)
	if _, ok := c.get("/x", touched); ok {
		t.Error("dir with a new mtime served from cache")
	}
	replaced := e
	replaced.Ino = 43
	if _, ok := c.get("/x", replaced); ok {
		t.Error("replaced dir served from cache")
	}
	c.m["/x"] = lump{Ino: e.Ino, Mod: e.ModTime.UnixNano(), At: time.Now().Add(-2 * lumpTTL).UnixNano(), Usage: u}
	if _, ok := c.get("/x", e); ok {
		t.Error("expired entry served from cache")
	}
	if (*lumpCache)(nil).save() != nil || openLumps("") != nil {
		t.Error("empty path should disable the cache")
	}
}
