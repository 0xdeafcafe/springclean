package scan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0xdeafcafe/springclean/internal/catalog"
	"github.com/0xdeafcafe/springclean/internal/domain"
	"github.com/dustin/go-humanize"
)

type Event interface{ isEvent() }

type SuspectEvent struct{ S domain.Suspect }
type ProgressEvent struct{ P domain.ScanProgress }
type ErrorEvent struct {
	Path string
	Err  error
}
type DoneEvent struct{ R domain.ScanResult }

func (SuspectEvent) isEvent()  {}
func (ProgressEvent) isEvent() {}
func (ErrorEvent) isEvent()    {}
func (DoneEvent) isEvent()     {}

type Config struct {
	Mode    domain.ScanMode
	Root    string
	Workers int
	MinSize int64

	// WorktreeAgeDays is the minimum untouched age for a linked git worktree to
	// be flagged. Zero uses catalog.WorktreeAgeDays; negative disables the
	// filter and reports every worktree found.
	WorktreeAgeDays int

	// IgnoredAgeDays is the minimum untouched age for gitignored cruft.
	// Zero uses catalog.IgnoredCruftAgeDays; negative disables the filter.
	IgnoredAgeDays int

	// ScanIgnored enables asking git for ignored-but-untracked paths in each
	// repository the walk crosses. Off by default: it costs a `git ls-files`
	// per repo, and the curated marker list already covers the common cases.
	ScanIgnored bool

	// CacheAgeDays hides build and dependency directories belonging to a
	// checkout somebody has worked on within this many days. Zero reports every
	// cache found, which is the default.
	CacheAgeDays int
}

type Scanner struct {
	cfg Config

	sem chan struct{}
	wg  sync.WaitGroup

	out chan<- Event
	ctx context.Context

	items         atomic.Int64
	bytes         atomic.Int64
	suspects      atomic.Int64
	suspBytes     atomic.Int64
	errCount      atomic.Int64
	skipped       atomic.Int64
	submitted     atomic.Int64
	completed     atomic.Int64
	activeWorkers atomic.Int64
	peakWorkers   atomic.Int64

	currentPath atomic.Value // string
	started     time.Time

	suspMu   sync.Mutex
	suspList []domain.Suspect

	seenMu sync.Mutex
	seen   map[string]bool

	ignoredMu      sync.Mutex
	ignoredScanned map[string]bool

	skipSet map[string]bool
	markers map[string]catalog.StopMarker
}

func New(cfg Config) *Scanner {
	if cfg.Workers <= 0 {
		// FS work is IO-bound — overcommit aggressively. APFS handles
		// concurrent metadata reads well; the OS scheduler tames any excess.
		cfg.Workers = runtime.NumCPU() * 16
	}
	if cfg.Workers < 32 {
		cfg.Workers = 32
	}
	if cfg.Workers > 256 {
		cfg.Workers = 256
	}
	if cfg.MinSize <= 0 {
		cfg.MinSize = 1 * 1024 * 1024 // 1 MB
	}
	skipSet := map[string]bool{}
	for _, p := range catalog.SkipDuringHomeWalk() {
		skipSet[p] = true
	}
	return &Scanner{
		cfg:     cfg,
		sem:     make(chan struct{}, cfg.Workers),
		seen:    map[string]bool{},
		skipSet: skipSet,
		markers: catalog.StopMarkers(),
	}
}

func (s *Scanner) Run(ctx context.Context) <-chan Event {
	out := make(chan Event, 256)
	s.out = out
	s.ctx = ctx
	go s.run(ctx, out)
	return out
}

func (s *Scanner) run(ctx context.Context, out chan<- Event) {
	defer close(out)

	s.started = time.Now()
	s.currentPath.Store("")

	// Progress ticker.
	stopProg := make(chan struct{})
	progDone := make(chan struct{})
	go func() {
		s.progressLoop(ctx, out, stopProg)
		close(progDone)
	}()

	// Seed jobs based on mode.
	switch s.cfg.Mode {
	case domain.ModeCurated:
		s.seedCurated()
		s.detectUnusedApps()
	case domain.ModeHome:
		s.seedCurated()
		s.detectUnusedApps()
		if home, err := os.UserHomeDir(); err == nil {
			s.walkRoot(home)
		}
	case domain.ModeRoot:
		root := s.cfg.Root
		if root == "" {
			root = "/"
		}
		s.walkRoot(root)
	}

	// Wait for all work to finish.
	s.wg.Wait()
	close(stopProg)
	<-progDone

	finished := time.Now()

	s.suspMu.Lock()
	result := domain.ScanResult{
		StartedAt:  s.started,
		FinishedAt: finished,
		Mode:       s.cfg.Mode,
		Root:       s.cfg.Root,
		Suspects:   append([]domain.Suspect(nil), s.suspList...),
		Stats: domain.Stats{
			TotalItems: s.items.Load(),
			TotalBytes: s.bytes.Load(),
			Skipped:    int(s.skipped.Load()),
			Errors:     int(s.errCount.Load()),
			Duration:   finished.Sub(s.started),
		},
	}
	s.suspMu.Unlock()

	select {
	case out <- DoneEvent{R: result}:
	case <-ctx.Done():
	}
}

func (s *Scanner) progressLoop(ctx context.Context, out chan<- Event, stop <-chan struct{}) {
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			s.emitProgress(true)
			return
		case <-t.C:
			s.emitProgress(false)
		}
	}
}

func (s *Scanner) emitProgress(done bool) {
	cur, _ := s.currentPath.Load().(string)
	submitted := s.submitted.Load()
	completed := s.completed.Load()
	active := s.activeWorkers.Load()
	queued := submitted - completed - active
	if queued < 0 {
		queued = 0
	}
	elapsed := time.Since(s.started)
	items := s.items.Load()
	bytes := s.bytes.Load()
	secs := elapsed.Seconds()
	var ips, bps float64
	if secs > 0 {
		ips = float64(items) / secs
		bps = float64(bytes) / secs
	}
	p := domain.ScanProgress{
		Phase:           "scanning",
		CurrentPath:     cur,
		ItemsSeen:       items,
		BytesSeen:       bytes,
		SuspectCount:    int(s.suspects.Load()),
		SuspectBytes:    s.suspBytes.Load(),
		Errors:          int(s.errCount.Load()),
		Skipped:         int(s.skipped.Load()),
		Done:            done,
		ActiveWorkers:   int(active),
		Queued:          int(queued),
		PeakWorkers:     int(s.peakWorkers.Load()),
		GoroutinesSpawn: submitted,
		Elapsed:         elapsed,
		ItemsPerSec:     ips,
		BytesPerSec:     bps,
	}
	select {
	case s.out <- ProgressEvent{P: p}:
	case <-s.ctx.Done():
	}
}

func (s *Scanner) submit(fn func()) {
	if s.ctx.Err() != nil {
		return
	}
	s.submitted.Add(1)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.completed.Add(1)
		select {
		case s.sem <- struct{}{}:
		case <-s.ctx.Done():
			return
		}
		defer func() { <-s.sem }()
		active := s.activeWorkers.Add(1)
		for {
			peak := s.peakWorkers.Load()
			if active <= peak || s.peakWorkers.CompareAndSwap(peak, active) {
				break
			}
		}
		defer s.activeWorkers.Add(-1)
		fn()
	}()
}

func (s *Scanner) markSeen(path string) bool {
	s.seenMu.Lock()
	defer s.seenMu.Unlock()
	if s.seen[path] {
		return false
	}
	s.seen[path] = true
	return true
}

func (s *Scanner) emit(suspect domain.Suspect) {
	if suspect.Size < s.cfg.MinSize {
		return
	}
	s.suspMu.Lock()
	s.suspList = append(s.suspList, suspect)
	s.suspMu.Unlock()
	s.suspects.Add(1)
	s.suspBytes.Add(suspect.Size)
	select {
	case s.out <- SuspectEvent{S: suspect}:
	case <-s.ctx.Done():
	}
}

func (s *Scanner) reportErr(path string, err error) {
	if errors.Is(err, fs.ErrPermission) {
		s.skipped.Add(1)
		return
	}
	s.errCount.Add(1)
	select {
	case s.out <- ErrorEvent{Path: path, Err: err}:
	case <-s.ctx.Done():
	}
}

func (s *Scanner) seedCurated() {
	for _, loc := range catalog.KnownLocations() {
		expanded := catalog.ExpandHome(loc.Path)
		if loc.IsContents {
			s.sizeContents(expanded, loc.Category, loc.Reason, loc.Regenerable)
		} else {
			s.sizeWhole(expanded, loc.Category, loc.Reason, loc.Regenerable)
		}
	}
}

func (s *Scanner) walkRoot(root string) {
	s.walkDir(root, project{})
}

// walkDir descends `dir`, carrying the checkout it is currently inside so that
// anything found underneath can be attributed to it.
func (s *Scanner) walkDir(dir string, proj project) {
	s.submit(func() {
		s.currentPath.Store(dir)
		if ok, gitDir := linkedWorktree(dir); ok {
			touched, emitted := s.emitWorktree(dir, gitDir)
			if emitted {
				// Emitted whole. Walking in would double-count everything
				// the worktree removal already reclaims.
				return
			}
			// Too recently touched to flag as abandoned. Walk it like any other
			// directory so its caches are still found, but remember how fresh
			// it is: its caches belong to work that's still in progress.
			proj = project{root: dir, touched: touched}
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			s.reportErr(dir, err)
			return
		}
		if hasGitEntry(entries) {
			if s.cfg.ScanIgnored {
				s.scanIgnored(dir)
			}
			// An ordinary checkout. Dating it from git's index is a stat rather
			// than the full walk emitWorktree can afford to do.
			if proj.root != dir {
				if touched := gitActivity(dir); !touched.IsZero() {
					proj = project{root: dir, touched: touched}
				}
			}
		}
		for _, e := range entries {
			path := filepath.Join(dir, e.Name())
			if s.skipSet[path] {
				s.skipped.Add(1)
				continue
			}
			s.items.Add(1)

			if e.IsDir() {
				if marker, ok := s.markers[e.Name()]; ok {
					if s.cacheSuppressed(proj) {
						s.skipped.Add(1)
						continue
					}
					s.sizeCache(path, marker.Category, marker.Reason, marker.Regenerable, proj)
					continue
				}
				if isHiddenSkip(e.Name()) {
					continue
				}
				s.walkDir(path, proj)
				continue
			}

			info, err := e.Info()
			if err != nil {
				continue
			}
			sz := info.Size()
			s.bytes.Add(sz)

			s.checkFileHeuristics(path, info)
		}
	})
}

func (s *Scanner) checkFileHeuristics(path string, info fs.FileInfo) {
	sz := info.Size()
	parent := filepath.Dir(path)

	if isInDownloads(parent) {
		age := time.Since(info.ModTime())
		if age > catalog.DownloadAgeDays*24*time.Hour && sz >= 10*1024*1024 {
			days := int(age.Hours() / 24)
			s.emit(domain.Suspect{
				ID:          domain.MakeID(path),
				Path:        path,
				Size:        sz,
				Category:    domain.CatDownload,
				Reason:      fmt.Sprintf("Download untouched for %d days", days),
				IsDir:       false,
				LastUsed:    info.ModTime(),
				Regenerable: false,
			})
			return
		}
	}

	if sz >= catalog.LargeFileThreshold {
		s.emit(domain.Suspect{
			ID:          domain.MakeID(path),
			Path:        path,
			Size:        sz,
			Category:    domain.CatLargeFile,
			Reason:      fmt.Sprintf("Large file (%s)", humanize.Bytes(uint64(sz))),
			IsDir:       false,
			LastUsed:    info.ModTime(),
			Regenerable: false,
		})
	}
}

// cacheSuppressed reports whether a cache should be left alone because the
// project owning it is still being worked on.
func (s *Scanner) cacheSuppressed(proj project) bool {
	minAge := s.cacheMinAge()
	if minAge <= 0 || !proj.known() {
		return false
	}
	return time.Since(proj.touched) < minAge
}

// cacheMinAge is how long a project must have been left alone before its
// caches are worth offering. Zero means no filter: every cache is reported,
// which is the behaviour when the flag isn't set.
func (s *Scanner) cacheMinAge() time.Duration {
	if s.cfg.CacheAgeDays <= 0 {
		return 0
	}
	return time.Duration(s.cfg.CacheAgeDays) * 24 * time.Hour
}

// sizeCache emits a build or dependency directory, tagged with the checkout it
// belongs to.
func (s *Scanner) sizeCache(path string, cat domain.Category, reason string, regen bool, proj project) {
	s.submit(func() {
		if !s.markSeen(path) {
			return
		}
		info, err := os.Lstat(path)
		if err != nil {
			if !os.IsNotExist(err) {
				s.reportErr(path, err)
			}
			return
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return
		}
		sus := domain.Suspect{
			ID:          domain.MakeID(path),
			Path:        path,
			Size:        s.sumDir(path),
			Category:    cat,
			Reason:      reason,
			IsDir:       true,
			LastUsed:    info.ModTime(),
			Regenerable: regen,
		}
		if proj.known() {
			sus.Project = proj.root
			sus.ProjectLastUsed = proj.touched
		}
		s.emit(sus)
	})
}

func (s *Scanner) sizeWhole(path string, cat domain.Category, reason string, regen bool) {
	s.submit(func() {
		if !s.markSeen(path) {
			return
		}
		s.currentPath.Store(path)
		info, err := os.Lstat(path)
		if err != nil {
			if !os.IsNotExist(err) {
				s.reportErr(path, err)
			}
			return
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return
		}
		var size int64
		if info.IsDir() {
			size = s.sumDir(path)
		} else {
			size = info.Size()
			s.bytes.Add(size)
			s.items.Add(1)
		}
		s.emit(domain.Suspect{
			ID:          domain.MakeID(path),
			Path:        path,
			Size:        size,
			Category:    cat,
			Reason:      reason,
			IsDir:       info.IsDir(),
			LastUsed:    info.ModTime(),
			Regenerable: regen,
		})
	})
}

func (s *Scanner) sizeContents(path string, cat domain.Category, reason string, regen bool) {
	s.submit(func() {
		s.currentPath.Store(path)
		entries, err := os.ReadDir(path)
		if err != nil {
			if !os.IsNotExist(err) {
				s.reportErr(path, err)
			}
			return
		}
		for _, e := range entries {
			child := filepath.Join(path, e.Name())
			childReason := reason
			if e.Name() != "" {
				childReason = fmt.Sprintf("%s · %s", reason, e.Name())
			}
			s.sizeWhole(child, cat, childReason, regen)
		}
	})
}

// sumDir walks `root` in parallel and returns the total size of regular files
// underneath. A local semaphore caps fan-out so this can't starve the global
// worker pool that called us. Per-directory atomic batching keeps cache
// contention low when many workers are active. Symlinks are not followed.
func (s *Scanner) sumDir(root string) int64 {
	var total atomic.Int64
	localSem := make(chan struct{}, 16)
	var wg sync.WaitGroup

	var walk func(string)
	walk = func(dir string) {
		defer wg.Done()
		select {
		case localSem <- struct{}{}:
		case <-s.ctx.Done():
			return
		}
		defer func() { <-localSem }()

		entries, err := os.ReadDir(dir)
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				s.skipped.Add(1)
			}
			return
		}
		var localBytes int64
		var localItems int64
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			if e.IsDir() {
				wg.Add(1)
				go walk(p)
				continue
			}
			localBytes += info.Size()
			localItems++
		}
		if localBytes > 0 || localItems > 0 {
			total.Add(localBytes)
			s.bytes.Add(localBytes)
			s.items.Add(localItems)
		}
	}
	wg.Add(1)
	walk(root)
	wg.Wait()
	return total.Load()
}

func isHiddenSkip(name string) bool {
	switch name {
	case ".git", ".hg", ".svn":
		return true
	}
	return false
}

// emitWorktree records `dir` as a single suspect for a git linked worktree,
// summing the working-tree size. Children are not walked separately — removing
// the worktree reclaims everything inside, including its node_modules / target
// / etc., so flagging them individually would double-count.
//
// Age comes from the newest real source file rather than the directory mtime,
// and git is consulted for unsaved work, so a worktree the user is mid-way
// through is never presented as abandoned.
// Returns when the worktree was last worked on, and whether it was emitted as
// a single suspect. A false second value means it was too recently touched to
// be considered abandoned and the caller should walk it normally.
func (s *Scanner) emitWorktree(dir, gitDir string) (time.Time, bool) {
	if !s.markSeen(dir) {
		return time.Time{}, true
	}
	if _, err := os.Lstat(dir); err != nil {
		if !os.IsNotExist(err) {
			s.reportErr(dir, err)
		}
		return time.Time{}, true
	}

	touched := contentModTime(s.ctx, dir)
	if touched.IsZero() {
		// Nothing datable inside, so fall back to the directory's own mtime
		// rather than treating the worktree as infinitely old.
		if info, err := os.Lstat(dir); err == nil {
			touched = info.ModTime()
		}
	}

	age := time.Since(touched)
	if age < s.worktreeMinAge() {
		return touched, false
	}

	size := s.sumDir(dir)
	git := readGitState(s.ctx, dir)
	days := int(age.Hours() / 24)

	reason := fmt.Sprintf("Linked git worktree, untouched for %d days", days)
	if git.Branch != "" {
		reason = fmt.Sprintf("%s (%s)", reason, git.Branch)
	}

	s.emit(domain.Suspect{
		ID:          domain.MakeID(dir),
		Path:        dir,
		Size:        size,
		Category:    domain.CatGitWorktree,
		Reason:      reason,
		IsDir:       true,
		LastUsed:    touched,
		Regenerable: false,
		Warning:     git.Warning(),
		GitDir:      gitDir,
	})
	return touched, true
}

// worktreeMinAge is how long a worktree must sit untouched before it's worth
// showing. Zero from the config means "use the catalog default"; a negative
// value disables the filter entirely.
func (s *Scanner) worktreeMinAge() time.Duration {
	switch {
	case s.cfg.WorktreeAgeDays < 0:
		return 0
	case s.cfg.WorktreeAgeDays == 0:
		return catalog.WorktreeAgeDays * 24 * time.Hour
	default:
		return time.Duration(s.cfg.WorktreeAgeDays) * 24 * time.Hour
	}
}

func isInDownloads(parent string) bool {
	home, _ := os.UserHomeDir()
	dl := filepath.Join(home, "Downloads")
	return parent == dl || strings.HasPrefix(parent, dl+string(filepath.Separator))
}
