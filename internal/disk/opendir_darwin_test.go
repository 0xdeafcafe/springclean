package disk

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
	"time"
)

func TestReadDirGivesUpOnAnOpenThatNeverReturns(t *testing.T) {
	release := make(chan struct{})
	origOpen, origTimeout := sysOpen, OpenTimeout
	sysOpen = func(string, int, uint32) (int, error) {
		<-release
		return -1, errors.New("released")
	}
	OpenTimeout = 20 * time.Millisecond
	defer func() {
		close(release)
		sysOpen, OpenTimeout = origOpen, origTimeout
	}()

	dir := t.TempDir()
	done := make(chan error, 1)
	go func() {
		_, err := ReadDir(dir)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("err = %v, want it to count as a permission refusal", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadDir is still waiting on the blocked open")
	}

	found := false
	for _, p := range TimedOut() {
		found = found || p == dir
	}
	if !found {
		t.Fatalf("%s missing from TimedOut(): %v", dir, TimedOut())
	}
}

func TestSiblingsOfATimedOutDirectoryAreRefusedAtOnce(t *testing.T) {
	release := make(chan struct{})
	origOpen, origTimeout := sysOpen, OpenTimeout
	sysOpen = func(string, int, uint32) (int, error) {
		<-release
		return -1, errors.New("released")
	}
	OpenTimeout = 20 * time.Millisecond
	defer func() {
		close(release)
		sysOpen, OpenTimeout = origOpen, origTimeout
	}()

	parent := t.TempDir()
	if _, err := ReadDir(filepath.Join(parent, "first")); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("first: err = %v", err)
	}

	OpenTimeout = time.Hour
	done := make(chan error, 1)
	go func() {
		_, err := ReadDir(filepath.Join(parent, "second"))
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("second: err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the sibling waited for its own timeout")
	}
}

func TestReadDirStillListsAnOrdinaryDirectory(t *testing.T) {
	if _, err := ReadDir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
}
