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
	CatDevCache  Category = "dev_cache"
	CatAppCache  Category = "app_cache"
	CatAppLog    Category = "app_log"
	CatXcode     Category = "xcode"
	CatPkgCache  Category = "pkg_cache"
	CatDocker    Category = "docker"
	CatTrash     Category = "trash"
	CatDownload  Category = "old_download"
	CatUnusedApp Category = "unused_app"
	CatLargeFile    Category = "large_file"
	CatDuplicate    Category = "duplicate"
	CatGitWorktree  Category = "git_worktree"
	CatIgnoredCruft Category = "ignored_cruft"
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
	}
	return "•"
}

func AllCategories() []Category {
	return []Category{
		CatDevCache, CatAppCache, CatAppLog, CatXcode, CatPkgCache,
		CatDocker, CatTrash, CatDownload, CatUnusedApp, CatLargeFile, CatDuplicate,
		CatGitWorktree, CatIgnoredCruft,
	}
}

type ScanMode int

const (
	ModeCurated ScanMode = iota
	ModeHome
	ModeRoot
)

func (m ScanMode) String() string {
	switch m {
	case ModeCurated:
		return "curated"
	case ModeHome:
		return "home"
	case ModeRoot:
		return "root"
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
