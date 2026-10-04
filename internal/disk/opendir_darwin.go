package disk

import (
	"io/fs"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"
)

// OpenTimeout bounds how long opening one directory may take.
//
// macOS holds the open() of a privacy-protected folder (another app's
// container, Desktop, a cloud drive) until somebody answers its consent
// dialog. On an unattended run nobody does, and the scan would wait forever.
var OpenTimeout = 5 * time.Second

// sysOpen is swapped out by tests to stand in for an open that never returns.
var sysOpen = syscall.Open

type openTimeoutError struct{}

func (openTimeoutError) Error() string { return "open timed out, likely waiting on a privacy prompt" }

// Is makes a timed-out open count as a permission refusal, which is what it
// is: the folder is skipped rather than reported as a failure.
func (openTimeoutError) Is(target error) bool { return target == fs.ErrPermission }

var (
	timedOutMu sync.Mutex
	timedOut   = map[string]bool{}
	// blocked holds parents with a child that timed out. Their other children
	// are refused at once: every app container raises its own prompt, and
	// waiting out hundreds of them in turn would take the scan hours.
	blocked = map[string]bool{}
)

// TimedOut lists the directories whose open never came back, so a caller can
// say what the scan had to leave out.
func TimedOut() []string {
	timedOutMu.Lock()
	defer timedOutMu.Unlock()
	out := make([]string, 0, len(timedOut))
	for p := range timedOut {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

type openResult struct {
	fd  int
	err error
}

func openDir(dir string) (int, error) {
	parent := filepath.Dir(dir)
	timedOutMu.Lock()
	refused := blocked[parent]
	if refused {
		timedOut[parent+"/*"] = true
	}
	timedOutMu.Unlock()
	if refused {
		return -1, openTimeoutError{}
	}

	ch := make(chan openResult, 1)
	go func() {
		fd, err := sysOpen(dir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
		ch <- openResult{fd, err}
	}()
	timer := time.NewTimer(OpenTimeout)
	defer timer.Stop()
	select {
	case r := <-ch:
		return r.fd, r.err
	case <-timer.C:
		timedOutMu.Lock()
		timedOut[dir] = true
		blocked[parent] = true
		timedOutMu.Unlock()
		// The open may still return once the prompt is answered; don't leak it.
		go func() {
			if r := <-ch; r.err == nil {
				syscall.Close(r.fd)
			}
		}()
		return -1, openTimeoutError{}
	}
}
