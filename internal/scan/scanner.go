package scan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/0xdeafcafe/springclean/internal/catalog"
	"github.com/0xdeafcafe/springclean/internal/disk"
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

	// SkipDedupe charges every copy of a file in full instead of working out
	// whether copies share their storage.
	//
	// Detecting that costs an open per file, and it is what keeps fifty pnpm
	// checkouts of one lockfile from looking like fifty times the disk they
	// actually take. Sizes stay block-accurate either way.
	SkipDedupe bool

	// CachePath is where measured lump sizes are remembered between scans.
	// Empty disables the cache.
	CachePath string
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

	// tree holds the bytes found directly in each walked directory (files, plus
	// anything sized as a lump), which the large-folder pass rolls up.
	treeMu sync.Mutex
	tree   map[string]int64

	// io caps directory reads made while sizing lumps, across every lump at
	// once. Each lump used to get its own cap, which added up to hundreds of
	// threads fighting over the same filesystem locks.
	io chan struct{}

	lumps *lumpCache

	// walked is the allocated size of the whole tree under the walk root.
	walked int64

	// devs are the devices a walk may enter. Anything else is another mount.
	devs map[int32]bool

	// claims records storage already counted, so a file cloned or hard linked
	// into several suspects is only charged to the first one that finds it.
	claims *disk.Claims
}

func New(cfg Config) *Scanner {
	if cfg.Workers <= 0 {
		// Directory listing is kernel work. Measured on APFS, six workers
		// finished as fast as eighty and every extra thread only took cores
		// away from everything else on the machine.
		cfg.Workers = runtime.NumCPU() / 2
	}
	if cfg.Workers < 4 {
		cfg.Workers = 4
	}
	if cfg.Workers > 256 {
		cfg.Workers = 256
	}
	if cfg.MinSize <= 0 {
		cfg.MinSize = 1 * 1024 * 1024 // 1 MB
	}
	skipSet := map[string]bool{}
	skips := catalog.SkipDuringHomeWalk()
	if cfg.Mode == domain.ModeFull {
		skips = catalog.SkipDuringFullWalk()
	}
	for _, p := range skips {
		skipSet[p] = true
	}
	return &Scanner{
		cfg:     cfg,
		sem:     make(chan struct{}, cfg.Workers),
		seen:    map[string]bool{},
		skipSet: skipSet,
		markers: catalog.StopMarkers(),
		claims:  disk.NewClaims(!cfg.SkipDedupe),
		tree:    map[string]int64{},
		io:      make(chan struct{}, max(2, runtime.NumCPU()/4)),
		lumps:   openLumps(cfg.CachePath),
		devs:    map[int32]bool{},
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
	var root string
	switch s.cfg.Mode {
	case domain.ModeCurated:
		s.seedCurated()
		s.detectUnusedApps()
		s.detectSimRuntimes()
	case domain.ModeHome:
		s.seedCurated()
		s.detectUnusedApps()
		s.detectSimRuntimes()
		root, _ = os.UserHomeDir()
	case domain.ModeFull:
		s.seedCurated()
		s.detectUnusedApps()
		s.detectSimRuntimes()
		root = "/"
	case domain.ModeRoot:
		root = s.cfg.Root
		if root == "" {
			root = "/"
		}
	}
	if root != "" {
		s.walkRoot(root)
	}

	// Wait for all work to finish.
	s.wg.Wait()
	if root != "" {
		s.emitLargeDirs(root)
		if ctx.Err() == nil {
			_ = s.lumps.save()
		}
	}
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
			TotalItems:  s.items.Load(),
			TotalBytes:  s.bytes.Load(),
			WalkedBytes: s.walked,
			Unreadable:  disk.TimedOut(),
			Skipped:     int(s.skipped.Load()),
			Errors:      int(s.errCount.Load()),
			Duration:    finished.Sub(s.started),
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
	if suspect.Footprint() < s.cfg.MinSize {
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
	// On macOS "/" is the sealed system volume and everything writable is
	// firmlinked in from the data volume, so both count as "this disk".
	for _, p := range []string{root, "/", "/System/Volumes/Data"} {
		if dev, ok := deviceOf(p); ok {
			s.devs[dev] = true
		}
	}
	s.walkDir(root, disk.Entry{}, project{})
}

func deviceOf(path string) (int32, bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, false
	}
	return statDev(info)
}

func statDev(info fs.FileInfo) (int32, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int32(st.Dev), true
}

func (s *Scanner) addToTree(dir string, n int64) {
	s.treeMu.Lock()
	s.tree[dir] += n
	s.treeMu.Unlock()
}

// sizeLeaf measures a directory as one lump without walking it for findings.
// Sharing isn't worked out: that costs an open per file, which is exactly the
// cost a lump is meant to avoid.
func (s *Scanner) sizeLeaf(path string, e disk.Entry) {
	s.submit(func() {
		s.currentPath.Store(path)
		s.addToTree(path, s.measureLump(path, e, nil).Real)
	})
}

// walkDir descends `dir`, carrying the checkout it is currently inside so that
// anything found underneath can be attributed to it.
// self is dir's own entry from its parent's listing, zero for the root.
func (s *Scanner) walkDir(dir string, self disk.Entry, proj project) {
	s.submit(func() {
		s.currentPath.Store(dir)
		entries, err := disk.ReadDir(dir)
		if err != nil {
			s.reportErr(dir, err)
			return
		}
		git, hasGit := gitEntry(entries)
		// Only a `.git` file can be a linked worktree, and the listing already
		// says whether there is one, so most directories cost no extra stat.
		if hasGit && git.Regular {
			if ok, gitDir := linkedWorktree(dir); ok {
				touched, emitted := s.emitWorktree(dir, self, gitDir)
				if emitted {
					// Emitted whole. Walking in would double-count everything
					// the worktree removal already reclaims.
					return
				}
				// Too recently touched to flag as abandoned. Walk it like any
				// other directory so its caches are still found, but remember
				// how fresh it is: its caches belong to work in progress.
				proj = project{root: dir, touched: touched}
			}
		}
		subdirs := 0
		for _, e := range entries {
			if e.IsDir {
				subdirs++
			}
		}
		if subdirs > catalog.FanoutLimit {
			s.addToTree(dir, s.measureLump(dir, self, nil).Real)
			return
		}
		s.addToTree(dir, 0)
		markers := s.cfg.Mode != domain.ModeFull || catalog.MarkersApply(dir)
		if markers && hasGit {
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
		var direct int64
		for _, e := range entries {
			path := filepath.Join(dir, e.Name)
			if s.skipSet[path] {
				s.skipped.Add(1)
				continue
			}
			s.items.Add(1)

			if e.IsDir {
				if !s.devs[e.Dev] {
					s.skipped.Add(1)
					continue
				}
				if marker, ok := s.markers[e.Name]; ok && markers {
					if s.cacheSuppressed(proj) {
						s.skipped.Add(1)
						continue
					}
					s.sizeCache(path, e, marker.Category, marker.Reason, marker.Regenerable, proj)
					continue
				}
				if isHiddenSkip(e.Name) || catalog.IsBundle(e.Name) {
					s.sizeLeaf(path, e)
					continue
				}
				s.walkDir(path, e, proj)
				continue
			}

			if !e.Regular {
				continue
			}
			s.bytes.Add(e.Size)
			direct += e.Alloc

			s.checkFileHeuristics(path, e)
		}
		s.addToTree(dir, direct)
	})
}

func (s *Scanner) checkFileHeuristics(path string, e disk.Entry) {
	sz := e.Size
	parent := filepath.Dir(path)

	if isInDownloads(parent) {
		age := time.Since(e.ModTime)
		if age > catalog.DownloadAgeDays*24*time.Hour && sz >= 10*1024*1024 {
			days := int(age.Hours() / 24)
			sus := domain.Suspect{
				ID:          domain.MakeID(path),
				Path:        path,
				Category:    domain.CatDownload,
				Reason:      fmt.Sprintf("Download untouched for %d days", days),
				IsDir:       false,
				LastUsed:    e.ModTime,
				Regenerable: false,
			}
			record(&sus, s.claims.ChargeEntry(path, e))
			s.emit(sus)
			return
		}
	}

	if sz >= catalog.LargeFileThreshold {
		sus := domain.Suspect{
			ID:          domain.MakeID(path),
			Path:        path,
			Category:    domain.CatLargeFile,
			Reason:      fmt.Sprintf("Large file (%s)", humanize.Bytes(uint64(sz))),
			IsDir:       false,
			LastUsed:    e.ModTime,
			Regenerable: false,
		}
		record(&sus, s.claims.ChargeEntry(path, e))
		s.emit(sus)
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
func (s *Scanner) sizeCache(path string, e disk.Entry, cat domain.Category, reason string, regen bool, proj project) {
	s.submit(func() {
		if !s.markSeen(path) {
			return
		}
		sus := domain.Suspect{
			ID:          domain.MakeID(path),
			Path:        path,
			Category:    cat,
			Reason:      reason,
			IsDir:       true,
			LastUsed:    e.ModTime,
			Regenerable: regen,
		}
		record(&sus, s.measureLump(path, e, s.claims))
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
		var usage disk.Usage
		if info.IsDir() {
			usage = s.sumDir(path)
		} else {
			usage = s.sumFile(path, info)
		}
		sus := domain.Suspect{
			ID:          domain.MakeID(path),
			Path:        path,
			Category:    cat,
			Reason:      reason,
			IsDir:       info.IsDir(),
			LastUsed:    info.ModTime(),
			Regenerable: regen,
		}
		record(&sus, usage)
		s.emit(sus)
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

// record puts a measurement on a suspect. Size is what deleting the item
// frees; Apparent is what its files add up to on paper.
func record(sus *domain.Suspect, u disk.Usage) {
	sus.Size = u.Real
	sus.Apparent = u.Apparent
	sus.Shared = u.Shared
}

// sumFile measures one regular file, counting it towards the scan totals.
func (s *Scanner) sumFile(path string, info os.FileInfo) disk.Usage {
	u := s.claims.Charge(path, info)
	s.bytes.Add(u.Apparent)
	s.items.Add(1)
	return u
}

// sumDir walks `root` in parallel and returns what the regular files
// underneath cost. A local semaphore caps fan-out so this can't starve the
// global worker pool that called us. Per-directory atomic batching keeps cache
// contention low when many workers are active. Symlinks are not followed.
func (s *Scanner) sumDir(root string) disk.Usage {
	return s.sumDirWith(root, s.claims)
}

func (s *Scanner) sumDirWith(root string, claims *disk.Claims) disk.Usage {
	var apparent, actual, shared atomic.Int64
	var wg sync.WaitGroup

	var walk func(string)
	walk = func(dir string) {
		defer wg.Done()
		select {
		case s.io <- struct{}{}:
		case <-s.ctx.Done():
			return
		}
		entries, err := disk.ReadDir(dir)
		<-s.io
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				s.skipped.Add(1)
			}
			if len(entries) == 0 {
				return
			}
		}
		var local disk.Usage
		var localItems int64
		for _, e := range entries {
			if e.Symlink {
				continue
			}
			p := filepath.Join(dir, e.Name)
			if e.IsDir {
				wg.Add(1)
				go walk(p)
				continue
			}
			local.Add(claims.ChargeEntry(p, e))
			localItems++
		}
		if local.Apparent > 0 || localItems > 0 {
			apparent.Add(local.Apparent)
			actual.Add(local.Real)
			shared.Add(local.Shared)
			s.bytes.Add(local.Apparent)
			s.items.Add(localItems)
		}
	}
	wg.Add(1)
	walk(root)
	wg.Wait()
	return disk.Usage{Apparent: apparent.Load(), Real: actual.Load(), Shared: shared.Load()}
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
func (s *Scanner) emitWorktree(dir string, self disk.Entry, gitDir string) (time.Time, bool) {
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

	// The size may come from the cache; the date above never does.
	usage := s.measureLump(dir, self, s.claims)
	git := readGitState(s.ctx, dir)
	days := int(age.Hours() / 24)

	reason := fmt.Sprintf("Linked git worktree, untouched for %d days", days)
	if git.Branch != "" {
		reason = fmt.Sprintf("%s (%s)", reason, git.Branch)
	}

	sus := domain.Suspect{
		ID:          domain.MakeID(dir),
		Path:        dir,
		Category:    domain.CatGitWorktree,
		Reason:      reason,
		IsDir:       true,
		LastUsed:    touched,
		Regenerable: false,
		Warning:     git.Warning(),
		GitDir:      gitDir,
	}
	record(&sus, usage)
	s.emit(sus)
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

// emitLargeDirs reports the folders holding a lot of disk that no other
// finding accounts for.
//
// Sizes roll up from the deepest directory. A folder is reported when at
// least catalog.LargeDirThreshold of it is not already explained by findings
// or by large folders beneath it, which lands on the tightest folder that
// actually holds the bulk: ~/.ollama/models rather than ~ or ~/.ollama.
//
// ponytail: sizes here are allocated blocks with no clone/hard-link dedupe, so
// a folder full of APFS clones reads larger than deleting it frees.
// largeDirThreshold is a var so tests can use megabytes rather than gigabytes.
var largeDirThreshold = catalog.LargeDirThreshold

func (s *Scanner) emitLargeDirs(root string) {
	if s.ctx.Err() != nil {
		return
	}
	direct := s.tree
	covered := map[string]int64{}
	listed := map[string]bool{}

	s.suspMu.Lock()
	found := append([]domain.Suspect(nil), s.suspList...)
	s.suspMu.Unlock()
	for _, sus := range found {
		if _, ok := direct[sus.Path]; ok {
			listed[sus.Path] = true
			continue
		}
		parent := filepath.Dir(sus.Path)
		if _, ok := direct[parent]; !ok {
			continue
		}
		if sus.IsDir {
			// Sized by its own finding rather than the walk.
			direct[parent] += sus.Footprint()
		}
		covered[parent] += sus.Footprint()
	}

	dirs := make([]string, 0, len(direct))
	for d := range direct {
		dirs = append(dirs, d)
	}
	depth := func(p string) int { return strings.Count(p, string(filepath.Separator)) }
	sort.Slice(dirs, func(i, j int) bool { return depth(dirs[i]) > depth(dirs[j]) })

	total := map[string]int64{}
	for _, d := range dirs {
		total[d] += direct[d]
		switch {
		case listed[d]:
			covered[d] = total[d]
		case d != root && !catalog.ProtectedDir(d) && total[d]-covered[d] >= largeDirThreshold:
			s.emitLargeDir(d, total[d], covered[d])
			covered[d] = total[d]
		}
		if d == root {
			continue
		}
		parent := filepath.Dir(d)
		total[parent] += total[d]
		covered[parent] += covered[d]
	}
	s.walked = total[root]
}

func (s *Scanner) emitLargeDir(path string, size, covered int64) {
	reason := "Large folder"
	if covered > 0 {
		reason = fmt.Sprintf("Large folder, %s in all (%s of it listed separately)",
			humanize.Bytes(uint64(size)), humanize.Bytes(uint64(covered)))
	}
	// Size is only the part no other finding lists, so totals across the
	// report don't count the same bytes twice. Deleting the folder frees all
	// of Apparent.
	sus := domain.Suspect{
		ID:       domain.MakeID(path),
		Path:     path,
		Category: domain.CatLargeDir,
		Reason:   reason,
		IsDir:    true,
		Size:     size - covered,
		Apparent: size,
	}
	if info, err := os.Lstat(path); err == nil {
		sus.LastUsed = info.ModTime()
	}
	s.emit(sus)
}
