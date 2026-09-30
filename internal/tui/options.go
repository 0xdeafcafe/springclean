package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/0xdeafcafe/springclean/internal/catalog"
	"github.com/0xdeafcafe/springclean/internal/domain"
	"github.com/0xdeafcafe/springclean/internal/scan"
)

// scopeChoice is the scope as the options screen presents it. "here" and
// "custom" both produce domain.ModeRoot; they differ only in whether the path
// is filled in for you.
type scopeChoice int

const (
	scopeFull scopeChoice = iota
	scopeCurated
	scopeHome
	scopeHere
	scopeCustom
)

func (s scopeChoice) Label() string {
	switch s {
	case scopeFull:
		return "full disk"
	case scopeCurated:
		return "curated"
	case scopeHome:
		return "home"
	case scopeHere:
		return "this folder"
	case scopeCustom:
		return "custom path"
	}
	return "?"
}

func (s scopeChoice) Blurb() string {
	switch s {
	case scopeFull:
		return "everything readable on this mac, including large folders nothing else names."
	case scopeCurated:
		return "known caches and unused apps. no walk."
	case scopeHome:
		return "the above, plus a full walk of $HOME."
	case scopeHere:
		return "walk the directory springclean started in."
	case scopeCustom:
		return "walk a directory you type in below."
	}
	return ""
}

// formField indexes the rows of the options screen.
type formField int

const (
	fieldScope formField = iota
	fieldPath
	fieldWorktreeAge
	fieldIgnored
	fieldIgnoredAge
	fieldCacheAge
	fieldRealSize
	fieldCount
)

type optionsForm struct {
	scope       scopeChoice
	path        string
	worktreeAge int
	ignored     bool
	ignoredAge  int
	cacheAge    int
	realSize    bool

	cursor  formField
	editing bool // typing into the path field
}

func newOptionsForm(cfg scan.Config) optionsForm {
	f := optionsForm{
		worktreeAge: cfg.WorktreeAgeDays,
		ignored:     cfg.ScanIgnored,
		ignoredAge:  cfg.IgnoredAgeDays,
		cacheAge:    cfg.CacheAgeDays,
		realSize:    !cfg.SkipDedupe,
	}
	if f.worktreeAge == 0 {
		f.worktreeAge = catalog.WorktreeAgeDays
	}
	if f.ignoredAge == 0 {
		f.ignoredAge = catalog.IgnoredCruftAgeDays
	}

	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	f.path = cwd

	switch cfg.Mode {
	case domain.ModeFull:
		f.scope = scopeFull
	case domain.ModeHome:
		f.scope = scopeHome
	case domain.ModeRoot:
		f.scope = scopeCustom
		if cfg.Root != "" {
			f.path = cfg.Root
			if cfg.Root == cwd {
				f.scope = scopeHere
			}
		}
	default:
		f.scope = scopeCurated
	}
	return f
}

// pathActive reports whether the path row applies to the current scope.
func (f optionsForm) pathActive() bool {
	return f.scope == scopeHere || f.scope == scopeCustom
}

// visible reports whether a row should be shown and reachable. Rows that don't
// apply to the current scope are skipped rather than shown greyed out, so the
// screen stays short enough to read at a glance.
func (f optionsForm) visible(field formField) bool {
	switch field {
	case fieldPath:
		return f.pathActive()
	case fieldWorktreeAge:
		// Worktrees are only found by a walking scope.
		return f.scope != scopeCurated
	case fieldIgnored:
		return f.scope != scopeCurated
	case fieldIgnoredAge:
		return f.scope != scopeCurated && f.ignored
	case fieldCacheAge:
		return f.scope != scopeCurated
	}
	return true
}

func (f *optionsForm) moveCursor(delta int) {
	for i := 0; i < int(fieldCount); i++ {
		next := int(f.cursor) + delta
		if next < 0 {
			next = int(fieldCount) - 1
		}
		if next >= int(fieldCount) {
			next = 0
		}
		f.cursor = formField(next)
		if f.visible(f.cursor) {
			return
		}
	}
}

// adjust changes the value under the cursor. delta is -1 or +1.
func (f *optionsForm) adjust(delta int) {
	switch f.cursor {
	case fieldScope:
		next := int(f.scope) + delta
		if next < 0 {
			next = int(scopeCustom)
		}
		if next > int(scopeCustom) {
			next = 0
		}
		f.scope = scopeChoice(next)
		if f.scope == scopeHere {
			if cwd, err := os.Getwd(); err == nil {
				f.path = cwd
			}
		}
		if !f.visible(f.cursor) {
			f.moveCursor(1)
		}
	case fieldWorktreeAge:
		f.worktreeAge = stepAge(f.worktreeAge, delta)
	case fieldIgnored:
		f.ignored = !f.ignored
	case fieldIgnoredAge:
		f.ignoredAge = stepAge(f.ignoredAge, delta)
	case fieldCacheAge:
		// 0 is meaningful here: it means every cache is reported.
		f.cacheAge += delta
		if f.cacheAge < 0 {
			f.cacheAge = 0
		}
		if f.cacheAge > 3650 {
			f.cacheAge = 3650
		}
	case fieldRealSize:
		f.realSize = !f.realSize
	}
}

// stepAge moves a threshold by one day, keeping it in a usable range.
//
// -1 is a real setting: it turns the age filter off so everything found is
// reported. 0 is not, because a zero in the config means "use the catalog
// default", so stepping onto it would silently jump the value somewhere else.
// It's skipped in whichever direction the user was heading.
func stepAge(v, delta int) int {
	v += delta
	if v == 0 {
		if delta < 0 {
			v = -1
		} else {
			v = 1
		}
	}
	if v < -1 {
		return -1
	}
	if v > 3650 {
		return 3650
	}
	return v
}

func ageLabel(days int) string {
	if days < 0 {
		return "no filter (report all)"
	}
	if days == 1 {
		return "1 day"
	}
	return itoa(days) + " days"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

// typePath edits the custom path in place.
func (f *optionsForm) typePath(s string) {
	f.path += s
}

func (f *optionsForm) backspacePath() {
	if f.path != "" {
		f.path = f.path[:len(f.path)-1]
	}
}

// config turns the form into a scan config, resolving the path the same way
// the CLI does so a typed `~/code` behaves the way it reads.
func (f optionsForm) config() (scan.Config, error) {
	cfg := scan.Config{
		WorktreeAgeDays: f.worktreeAge,
		IgnoredAgeDays:  f.ignoredAge,
		ScanIgnored:     f.ignored,
		CacheAgeDays:    f.cacheAge,
		SkipDedupe:      !f.realSize,
	}
	switch f.scope {
	case scopeFull:
		cfg.Mode = domain.ModeFull
	case scopeCurated:
		cfg.Mode = domain.ModeCurated
		// Category-specific options don't apply without a walk.
		cfg.ScanIgnored = false
		cfg.CacheAgeDays = 0
	case scopeHome:
		cfg.Mode = domain.ModeHome
	case scopeHere, scopeCustom:
		cfg.Mode = domain.ModeRoot
		root, err := resolveFormPath(f.path)
		if err != nil {
			return scan.Config{}, err
		}
		cfg.Root = root
	}
	return cfg, nil
}

// resolveFormPath mirrors the CLI's root resolution: expand `~`, make it
// absolute, and refuse a path that isn't a directory that exists. Scanning a
// path that isn't there reports nothing at all, which reads as "your disk is
// clean" rather than "that path is wrong".
func resolveFormPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", errEmptyPath
	}
	p = catalog.ExpandHome(p)
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errNotDir
	}
	return abs, nil
}

type formError string

func (e formError) Error() string { return string(e) }

const (
	errEmptyPath = formError("path is empty")
	errNotDir    = formError("path is not a directory")
)
