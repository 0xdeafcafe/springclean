//go:build !darwin

package disk

import "errors"

// errNoExtents keeps the build green off macOS. Nothing else supports
// clonefile in the way springclean cares about, so every file is charged its
// allocated size and hard links are still deduplicated by inode.
var errNoExtents = errors.New("physical extents unavailable on this platform")

func physicalOffset(string) (int64, error) { return 0, errNoExtents }
