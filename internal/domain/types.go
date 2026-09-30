package domain

import (
	"crypto/sha256"
	"encoding/base32"
	"path/filepath"
	"strings"
	"time"
)

type Category string

const (
	CatDevCache     Category = "dev_cache"
	CatAppCache     Category = "app_cache"
	CatAppLog       Category = "app_log"
	CatXcode        Category = "xcode"
	CatPkgCache     Category = "pkg_cache"
	CatDocker       Category = "docker"
	CatTrash        Category = "trash"
	CatDownload     Category = "old_download"
	CatUnusedApp    Category = "unused_app"
	CatLargeFile    Category = "large_file"
	CatDuplicate    Category = "duplicate"
	CatGitWorktree  Category = "git_worktree"
	CatIgnoredCruft Category = "ignored_cruft"
	CatLargeDir     Category = "large_dir"
	CatSimRuntime   Category = "sim_runtime"
)

func (c Category) Label() string {
	switch c {
	case CatDevCache:
		return "Dev cache"
	case CatAppCache:
		return "App cache"
	case CatAppLog:
		return "App logs"
	case CatXcode:
		return "Xcode"
	case CatPkgCache:
		return "Package cache"
	case CatDocker:
		return "Docker"
	case CatTrash:
		return "Trash"
	case CatDownload:
		return "Old download"
	case CatUnusedApp:
		return "Unused app"
	case CatLargeFile:
		return "Large file"
	case CatDuplicate:
		return "Duplicate"
	case CatGitWorktree:
		return "Git worktree"
	case CatIgnoredCruft:
		return "Ignored cruft"
	case CatLargeDir:
		return "Large folder"
	case CatSimRuntime:
		return "Simulator runtime"
	}
	return string(c)
}

func (c Category) Glyph() string {
	switch c {
	case CatDevCache:
		return "❀"
	case CatAppCache:
		return "✿"
	case CatAppLog:
		return "✾"
	case CatXcode:
		return "✽"
	case CatPkgCache:
		return "❁"
	case CatDocker:
		return "✼"
	case CatTrash:
		return "❃"
	case CatDownload:
		return "❋"
	case CatUnusedApp:
		return "✺"
	case CatLargeFile:
		return "✦"
	case CatDuplicate:
		return "✧"
	case CatGitWorktree:
		return "❂"
	case CatIgnoredCruft:
		return "❉"
	case CatLargeDir:
		return "❖"
	case CatSimRuntime:
		return "✥"
	}
	return "•"
}

func AllCategories() []Category {
	return []Category{
		CatDevCache, CatAppCache, CatAppLog, CatXcode, CatPkgCache,
		CatDocker, CatTrash, CatDownload, CatUnusedApp, CatLargeFile, CatDuplicate,
		CatGitWorktree, CatIgnoredCruft, CatLargeDir, CatSimRuntime,
	}
}

type ScanMode int

const (
	ModeCurated ScanMode = iota
	ModeHome
	ModeRoot
	// ModeFull is curated plus a walk of every readable directory on the
	// machine, reporting large folders no catalog entry would name.
	ModeFull
)

func (m ScanMode) String() string {
	switch m {
	case ModeCurated:
		return "curated"
	case ModeHome:
		return "home"
	case ModeRoot:
		return "root"
	case ModeFull:
		return "full"
	}
	return "unknown"
}

func ParseScanMode(s string) (ScanMode, bool) {
	switch s {
	case "curated", "":
		return ModeCurated, true
	case "home":
		return ModeHome, true
	case "root":
		return ModeRoot, true
	case "full":
		return ModeFull, true
	}
	return 0, false
}

type Suspect struct {
	ID          string    `yaml:"id"`
	Path        string    `yaml:"path"`
	Size        int64     `yaml:"size_bytes"`
	Category    Category  `yaml:"category"`
	Reason      string    `yaml:"reason"`
	IsDir       bool      `yaml:"is_dir"`
	LastUsed    time.Time `yaml:"last_used,omitempty"`
	Marked      bool      `yaml:"marked"`
	Regenerable bool      `yaml:"regenerable"`

	// Warning carries a per-item safety note, such as unsaved work that would be
	// lost. Non-empty means the item needs a look before it's marked.
	Warning string `yaml:"warning,omitempty"`

	// GitDir is the `.git/worktrees/<name>` admin directory backing a
	// CatGitWorktree suspect. Trashing the working tree leaves this behind, so
	// apply uses it to prune the stale registration from the owning repo.
	GitDir string `yaml:"git_dir,omitempty"`

	// Project is the checkout this item was found inside, and ProjectLastUsed
	// is when that checkout was last worked on. A cache belonging to a project
	// somebody touched this morning is a very different proposition to the
	// same cache under a branch nobody has opened since spring, and the cache's
	// own mtime says only when it was last installed.
	Project         string    `yaml:"project,omitempty"`
	ProjectLastUsed time.Time `yaml:"project_last_used,omitempty"`

	// Apparent is the sum of the logical file sizes underneath, and Shared is
	// the part of the real footprint that lives in blocks another suspect also
	// references, through hard links or APFS clones.
	//
	// Size is what deleting this frees; Apparent is what a directory listing
	// adds up to. They diverge wildly for anything a package manager installed
	// by cloning: fifty pnpm checkouts of one lockfile are fifty times the
	// apparent size and one time the real one.
	Apparent int64 `yaml:"apparent_bytes,omitempty"`
	Shared   int64 `yaml:"shared_bytes,omitempty"`
}

// Duplicated reports whether most of this item's bytes are storage something
// else in the same scan also holds, which means deleting it frees far less
// than its apparent size suggests.
func (s Suspect) Duplicated() bool {
	return s.Shared > 0 && s.Shared > s.Size
}

// Footprint is how much room the item takes up, as opposed to how much
// deleting it would free. The two differ for a copy that shares its storage
// with another one.
//
// Size floors are judged on this. The fiftieth clone of a node_modules frees
// nothing on its own, but it is still a gigabyte of files, and every copy has
// to go before any of the space comes back, so hiding all but the first would
// leave a pile of disk unreclaimable and invisible.
func (s Suspect) Footprint() int64 {
	// Size and Shared are both allocated bytes, and together they are what the
	// item occupies. Apparent can still be the larger of the two on a
	// filesystem that compresses, so it gets a look in.
	if alloc := s.Size + s.Shared; alloc > s.Apparent {
		return alloc
	}
	return s.Apparent
}

// MainRepoFromGitDir maps a linked worktree's admin directory back to the
// repository that owns it: `<repo>/.git/worktrees/<name>` → `<repo>`.
// Returns "" when the path isn't shaped like a worktree admin dir.
func MainRepoFromGitDir(gitDir string) string {
	const sep = "/.git/worktrees/"
	slashed := filepath.ToSlash(gitDir)
	idx := strings.LastIndex(slashed, sep)
	if idx < 0 {
		return ""
	}
	return filepath.FromSlash(slashed[:idx])
}

func MakeID(path string) string {
	sum := sha256.Sum256([]byte(path))
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:8])
}

type ScanProgress struct {
	Phase           string
	CurrentPath     string
	ItemsSeen       int64
	BytesSeen       int64
	SuspectCount    int
	SuspectBytes    int64
	Errors          int
	Skipped         int
	Done            bool
	ActiveWorkers   int
	Queued          int
	PeakWorkers     int
	GoroutinesSpawn int64
	Elapsed         time.Duration
	ItemsPerSec     float64
	BytesPerSec     float64
}

type Stats struct {
	TotalItems int64
	TotalBytes int64
	Skipped    int
	Errors     int
	Duration   time.Duration
}

type ScanResult struct {
	StartedAt  time.Time
	FinishedAt time.Time
	Mode       ScanMode
	Root       string
	Suspects   []Suspect
	Stats      Stats
}

func (r *ScanResult) TotalReclaimable() int64 {
	var total int64
	for _, s := range r.Suspects {
		total += s.Size
	}
	return total
}

func (r *ScanResult) MarkedReclaimable() int64 {
	var total int64
	for _, s := range r.Suspects {
		if s.Marked {
			total += s.Size
		}
	}
	return total
}

func (r *ScanResult) MarkedCount() int {
	n := 0
	for _, s := range r.Suspects {
		if s.Marked {
			n++
		}
	}
	return n
}
