//go:build !darwin

package disk

// ReadDir lists dir. Only macOS has a bulk call; elsewhere it's readdir+lstat.
func ReadDir(dir string) ([]Entry, error) { return readDirPortable(dir) }

// TimedOut is always empty here: only macOS holds an open behind a prompt.
func TimedOut() []string { return nil }
