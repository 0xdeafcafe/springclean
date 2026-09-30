// Package disk measures what a file actually costs on disk, which is not the
// same as how big it says it is.
//
// Three things make the logical size a lie:
//
//   - Allocation. A 9-byte file still occupies a whole block.
//   - Hard links. The same blocks, reachable under several names.
//   - Clones. APFS clonefile() gives two files their own inodes while they
//     share physical extents, and charges both the full block count. This is
//     how pnpm installs a package into fifty checkouts, and why du reports
//     fifty times the space that deleting all fifty would free.
//
// A Claims tracks the storage a scan has already counted, so the second copy
// of something is charged what it really costs, which is nothing.
package disk

import (
	"hash/maphash"
	"os"
	"sync"
	"syscall"
)

// Usage is what one file, or one tree, costs.
type Usage struct {
	// Apparent is the sum of logical file sizes: the number Finder shows and
	// the one springclean used to report.
	Apparent int64
	// Real is the allocated bytes that nothing else in this scan had already
	// claimed. Summed over a set of items, it is what deleting that set frees.
	Real int64
	// Shared is allocated bytes some other item in the same scan got to first,
	// so deleting this one alone would not free them.
	Shared int64
}

// Add accumulates another measurement into this one.
func (u *Usage) Add(o Usage) {
	u.Apparent += o.Apparent
	u.Real += o.Real
	u.Shared += o.Shared
}

// shards keep the claim maps from becoming a single lock the whole scan
// queues behind. The walk runs on every core it can get.
const shards = 64

type shard struct {
	mu     sync.Mutex
	inodes map[inode]struct{}
	blocks map[int64]struct{}
	clones map[uint64]struct{}
}

type inode struct {
	dev int32
	ino uint64
}

// Claims records the storage a scan has counted so far.
//
// The zero value is not usable; call NewClaims. A nil *Claims charges every
// file its allocated size, which is what you want when sharing is not being
// tracked.
type Claims struct {
	seed   maphash.Seed
	shard  [shards]shard
	extent bool
}

// NewClaims returns a claim set. extents enables asking the filesystem where
// each file physically lives, which is what catches clones; it costs an open
// per file, so it is a choice the caller makes.
func NewClaims(extents bool) *Claims {
	c := &Claims{seed: maphash.MakeSeed(), extent: extents}
	for i := range c.shard {
		c.shard[i].inodes = map[inode]struct{}{}
		c.shard[i].blocks = map[int64]struct{}{}
		c.shard[i].clones = map[uint64]struct{}{}
	}
	return c
}

// Charge measures one regular file and records what it consumed, returning
// zero Real for storage this scan has already counted.
func (c *Claims) Charge(path string, info os.FileInfo) Usage {
	u := Usage{Apparent: info.Size()}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		// Nothing better to go on than the logical size.
		u.Real = u.Apparent
		return u
	}
	alloc := int64(st.Blocks) * 512
	if alloc == 0 {
		// Empty, or small enough to live inside its own inode record.
		return u
	}
	if c == nil {
		u.Real = alloc
		return u
	}

	// A hard link is the same blocks under another name. Counting it twice
	// would be as wrong as counting a clone twice, and it is much cheaper to
	// detect: no syscall at all, the inode number is already in hand.
	if st.Nlink > 1 && !c.takeInode(inode{dev: int32(st.Dev), ino: uint64(st.Ino)}) {
		u.Shared = alloc
		return u
	}

	if c.extent {
		if off, err := physicalOffset(path); err == nil && off != 0 {
			if !c.takeBlock(off) {
				u.Shared = alloc
				return u
			}
		}
	}

	u.Real = alloc
	return u
}

// Enabled reports whether this claim set is tracking shared storage at all.
func (c *Claims) Enabled() bool { return c != nil && c.extent }

func (c *Claims) takeInode(k inode) bool {
	var h maphash.Hash
	h.SetSeed(c.seed)
	var buf [12]byte
	putUint32(buf[0:4], uint32(k.dev))
	putUint64(buf[4:12], k.ino)
	h.Write(buf[:])
	s := &c.shard[h.Sum64()%shards]

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, seen := s.inodes[k]; seen {
		return false
	}
	s.inodes[k] = struct{}{}
	return true
}

func (c *Claims) takeBlock(off int64) bool {
	s := &c.shard[uint64(off)/4096%shards]

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, seen := s.blocks[off]; seen {
		return false
	}
	s.blocks[off] = struct{}{}
	return true
}

func putUint32(b []byte, v uint32) {
	b[0], b[1], b[2], b[3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
}

func putUint64(b []byte, v uint64) {
	putUint32(b[0:4], uint32(v))
	putUint32(b[4:8], uint32(v>>32))
}
