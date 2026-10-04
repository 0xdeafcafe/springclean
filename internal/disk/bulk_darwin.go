package disk

import (
	"encoding/binary"
	"os"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// getattrlistbulk(2) returns a batch of directory entries together with the
// attributes asked for, so listing a directory and sizing everything in it is
// a handful of syscalls instead of a readdir plus an lstat per entry. On APFS
// it also hands back each file's clone ID, which is what makes clone
// detection possible without opening every file.
//
// The private size and clone refcount attributes would say more, but APFS
// works them out from the extent tree: measured, they cost 40% on top of the
// whole walk, while the clone ID is free.
const sysGetattrlistbulk = 461

const (
	attrBitMapCount = 5

	attrCmnReturnedAttrs = 0x80000000
	attrCmnName          = 0x00000001
	attrCmnDevID         = 0x00000002
	attrCmnObjType       = 0x00000008
	attrCmnModTime       = 0x00000400
	attrCmnFileID        = 0x02000000
	attrCmnError         = 0x20000000

	attrFileLinkCount = 0x00000001
	attrFileDataLen   = 0x00000200
	attrFileAllocSize = 0x00000004

	// With fsoptAttrCmnExtended the fork attribute word selects this instead.
	attrCmnExtCloneID = 0x00000100

	fsoptNoFollow        = 0x1
	fsoptPackInvalAttrs  = 0x8
	fsoptAttrCmnExtended = 0x20

	vReg = 1
	vDir = 2
	vLnk = 5
)

type attrList struct {
	bitmapCount uint16
	reserved    uint16
	commonAttr  uint32
	volAttr     uint32
	dirAttr     uint32
	fileAttr    uint32
	forkAttr    uint32
}

// ReadDir lists dir with getattrlistbulk.
func ReadDir(dir string) ([]Entry, error) {
	fd, err := openDir(dir)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: dir, Err: err}
	}
	defer syscall.Close(fd)

	al := attrList{
		bitmapCount: attrBitMapCount,
		commonAttr: attrCmnReturnedAttrs | attrCmnName | attrCmnDevID | attrCmnObjType |
			attrCmnModTime | attrCmnFileID | attrCmnError,
		fileAttr: attrFileLinkCount | attrFileAllocSize | attrFileDataLen,
		forkAttr: attrCmnExtCloneID,
	}
	bufp := bufPool.Get().(*[]byte)
	defer bufPool.Put(bufp)
	buf := *bufp
	var out []Entry
	for {
		n, _, errno := syscall.Syscall6(sysGetattrlistbulk, uintptr(fd),
			uintptr(unsafe.Pointer(&al)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)),
			fsoptNoFollow|fsoptPackInvalAttrs|fsoptAttrCmnExtended, 0)
		if errno != 0 {
			if errno == syscall.EINTR {
				continue
			}
			if len(out) == 0 && (errno == syscall.ENOTSUP || errno == syscall.EINVAL) {
				return readDirPortable(dir)
			}
			return out, &os.PathError{Op: "getattrlistbulk", Path: dir, Err: errno}
		}
		if n == 0 {
			return out, nil
		}
		p := buf
		for i := 0; i < int(n); i++ {
			recLen := binary.LittleEndian.Uint32(p)
			if e, ok := parseEntry(p[:recLen]); ok {
				out = append(out, e)
			}
			p = p[recLen:]
		}
	}
}

// parseEntry decodes one record: length, returned-attrs set, error, then
// common, file and extended attributes in bit order. FSOPT_PACK_INVAL_ATTRS
// gives every requested attribute a slot, so the layout only varies by type.
func parseEntry(rec []byte) (Entry, bool) {
	le := binary.LittleEndian
	var returned attrList // reuse layout: 5 words
	returned.commonAttr = le.Uint32(rec[4:])
	returned.fileAttr = le.Uint32(rec[4+12:])
	returned.forkAttr = le.Uint32(rec[4+16:])
	off := 4 + 20
	if le.Uint32(rec[off:]) != 0 { // per-entry error
		return Entry{}, false
	}
	off += 4

	var e Entry
	nameOff := int32(le.Uint32(rec[off:]))
	nameLen := le.Uint32(rec[off+4:])
	start := off + int(nameOff)
	if nameLen > 0 && start+int(nameLen) <= len(rec) {
		e.Name = string(rec[start : start+int(nameLen)-1]) // drop NUL
	}
	off += 8
	e.Dev = int32(le.Uint32(rec[off:]))
	off += 4
	switch le.Uint32(rec[off:]) {
	case vDir:
		e.IsDir = true
	case vLnk:
		e.Symlink = true
	case vReg:
		e.Regular = true
	}
	off += 4
	e.ModTime = time.Unix(int64(le.Uint64(rec[off:])), int64(le.Uint64(rec[off+8:])))
	off += 16
	e.Ino = le.Uint64(rec[off:])
	off += 8

	// The kernel leaves the file block out of directory records altogether,
	// pack-invalid or not.
	if !e.IsDir {
		e.Nlink = le.Uint32(rec[off:])
		off += 4
		e.Alloc = int64(le.Uint64(rec[off:]))
		off += 8
		e.Size = int64(le.Uint64(rec[off:]))
		off += 8
	}

	e.CloneID = le.Uint64(rec[off:])
	e.HasClone = returned.forkAttr&attrCmnExtCloneID != 0
	return e, e.Name != "" && e.Name != "." && e.Name != ".."
}

// Directories are listed on every core at once, so the read buffers are
// shared rather than allocated per directory.
var bufPool = sync.Pool{New: func() any { b := make([]byte, 128*1024); return &b }}
