package disk

import (
	"encoding/binary"
	"syscall"
	"unsafe"
)

// F_LOG2PHYS_EXT maps a logical range of a file onto the device, which is the
// only way to tell a clone from a copy: clonefile() leaves both files pointing
// at the same physical extent, while every other kind of duplicate gets its
// own.
const fLog2PhysExt = 65

// struct log2phys is declared under `#pragma pack(4)` in <sys/fcntl.h>, so it
// is 20 bytes with no padding after the uint32, not the 24 a Go struct with
// natural alignment would produce. Getting this wrong reads the two off_t
// fields four bytes shifted and yields offsets that collide constantly.
const (
	l2pSize        = 20
	l2pContigBytes = 4
	l2pDevOffset   = 12
)

// physicalOffset returns the device offset of a file's first extent. Files
// sharing an offset share storage.
func physicalOffset(path string) (int64, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_SYMLINK, 0)
	if err != nil {
		return 0, err
	}
	defer syscall.Close(fd)

	var buf [l2pSize]byte
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), fLog2PhysExt, uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return 0, errno
	}
	return int64(binary.LittleEndian.Uint64(buf[l2pDevOffset : l2pDevOffset+8])), nil
}
