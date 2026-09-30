package disk

import (
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Entry is one directory entry with everything the scan needs to know about it.
type Entry struct {
	Name    string
	IsDir   bool
	Symlink bool
	Regular bool
	Dev     int32
	Ino     uint64
	Nlink   uint32
	Size    int64 // logical
	Alloc   int64 // allocated on disk
	ModTime time.Time

	// CloneID is shared by files that are clones of one another, and is only
	// meaningful when HasClone is set, which is APFS.
	CloneID  uint64
	HasClone bool
}

// readDirPortable is ReadDir built from readdir plus an lstat per entry, for
// platforms and filesystems without getattrlistbulk.
func readDirPortable(dir string) ([]Entry, error) {
	des, err := os.ReadDir(dir)
	out := make([]Entry, 0, len(des))
	for _, de := range des {
		info, lerr := os.Lstat(filepath.Join(dir, de.Name()))
		if lerr != nil {
			continue
		}
		e := Entry{
			Name:    de.Name(),
			IsDir:   info.IsDir(),
			Symlink: info.Mode()&os.ModeSymlink != 0,
			Regular: info.Mode().IsRegular(),
			Size:    info.Size(),
			Alloc:   info.Size(),
			ModTime: info.ModTime(),
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			e.Dev = int32(st.Dev)
			e.Ino = uint64(st.Ino)
			e.Nlink = uint32(st.Nlink)
			e.Alloc = int64(st.Blocks) * 512
		}
		out = append(out, e)
	}
	return out, err
}

// ChargeEntry is Charge for an entry that came from ReadDir. Clones are
// recognised from the clone ID the filesystem already returned, so nothing
// has to be opened. The first file of a clone family is charged in full and
// the rest as shared, the same call the old per-file extent lookup made.
func (c *Claims) ChargeEntry(path string, e Entry) Usage {
	u := Usage{Apparent: e.Size}
	if e.Alloc == 0 {
		return u
	}
	if c == nil {
		u.Real = e.Alloc
		return u
	}
	if e.Nlink > 1 && !c.takeInode(inode{dev: e.Dev, ino: e.Ino}) {
		u.Shared = e.Alloc
		return u
	}
	if c.extent {
		switch {
		case e.HasClone:
			if !c.takeClone(e.CloneID) {
				u.Shared = e.Alloc
				return u
			}
		default:
			// No clone attributes (not APFS): fall back to asking where the
			// file physically lives.
			if off, err := physicalOffset(path); err == nil && off != 0 && !c.takeBlock(off) {
				u.Shared = e.Alloc
				return u
			}
		}
	}
	u.Real = e.Alloc
	return u
}

func (c *Claims) takeClone(id uint64) bool {
	s := &c.shard[id%shards]
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, seen := s.clones[id]; seen {
		return false
	}
	s.clones[id] = struct{}{}
	return true
}
