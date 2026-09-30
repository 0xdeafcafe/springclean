package scan

import (
	"encoding/gob"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/0xdeafcafe/springclean/internal/disk"
)

// lumpTTL is how long a measured lump is trusted. A lump's own inode and
// mtime catch most changes, since installing or removing packages rewrites
// the top level, but not a file growing three levels down, so the answer is
// also only reused for a day.
const lumpTTL = 24 * time.Hour

// lump is the remembered size of a directory the scan measures whole:
// node_modules and friends, bundles, .git, worktrees, and stores too wide to
// walk.
type lump struct {
	Ino   uint64
	Mod   int64
	At    int64
	Usage disk.Usage
}

// lumpCache remembers lump sizes between scans. Only sizes: when a worktree
// was last worked on is always worked out fresh, because a stale answer there
// would make live work look abandoned.
// A nil *lumpCache caches nothing.
type lumpCache struct {
	path string
	mu   sync.Mutex
	m    map[string]lump
}

func openLumps(path string) *lumpCache {
	if path == "" {
		return nil
	}
	c := &lumpCache{path: path, m: map[string]lump{}}
	if f, err := os.Open(path); err == nil {
		// A cache that won't decode is just a cold cache.
		_ = gob.NewDecoder(f).Decode(&c.m)
		f.Close()
	}
	return c
}

func (c *lumpCache) get(path string, e disk.Entry) (disk.Usage, bool) {
	if c == nil || e.Ino == 0 {
		return disk.Usage{}, false
	}
	c.mu.Lock()
	l, ok := c.m[path]
	c.mu.Unlock()
	if !ok || l.Ino != e.Ino || l.Mod != e.ModTime.UnixNano() || time.Since(time.Unix(0, l.At)) > lumpTTL {
		return disk.Usage{}, false
	}
	return l.Usage, true
}

func (c *lumpCache) put(path string, e disk.Entry, u disk.Usage) {
	if c == nil || e.Ino == 0 {
		return
	}
	c.mu.Lock()
	c.m[path] = lump{Ino: e.Ino, Mod: e.ModTime.UnixNano(), At: time.Now().UnixNano(), Usage: u}
	c.mu.Unlock()
}

// save writes the cache back, dropping anything past its TTL so paths that
// no longer exist don't accumulate forever.
func (c *lumpCache) save() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for p, l := range c.m {
		if time.Since(time.Unix(0, l.At)) > lumpTTL {
			delete(c.m, p)
		}
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := gob.NewEncoder(f).Encode(c.m); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// measureLump sizes a directory whole, from the cache when it can.
func (s *Scanner) measureLump(path string, e disk.Entry, claims *disk.Claims) disk.Usage {
	if u, ok := s.lumps.get(path, e); ok {
		s.bytes.Add(u.Apparent)
		return u
	}
	u := s.sumDirWith(path, claims)
	if s.ctx.Err() == nil {
		s.lumps.put(path, e, u)
	}
	return u
}
