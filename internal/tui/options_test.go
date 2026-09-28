package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xdeafcafe/springclean/internal/domain"
	"github.com/0xdeafcafe/springclean/internal/scan"
	tea "github.com/charmbracelet/bubbletea"
)

func TestNewOptionsFormDefaults(t *testing.T) {
	f := newOptionsForm(scan.Config{})
	if f.scope != scopeCurated {
		t.Errorf("scope = %v, want curated", f.scope)
	}
	// A zero threshold in the config means "use the catalog default", and the
	// form has to show that number rather than a literal 0.
	if f.worktreeAge <= 0 {
		t.Errorf("worktreeAge = %d, want the catalog default", f.worktreeAge)
	}
	if f.ignoredAge <= 0 {
		t.Errorf("ignoredAge = %d, want the catalog default", f.ignoredAge)
	}
	if f.path == "" {
		t.Error("path should default to the working directory")
	}
}

func TestNewOptionsFormReflectsConfig(t *testing.T) {
	dir := t.TempDir()
	f := newOptionsForm(scan.Config{
		Mode:            domain.ModeRoot,
		Root:            dir,
		WorktreeAgeDays: 30,
		ScanIgnored:     true,
		IgnoredAgeDays:  60,
	})
	if f.scope != scopeCustom {
		t.Errorf("scope = %v, want custom for an explicit root", f.scope)
	}
	if f.path != dir {
		t.Errorf("path = %q, want %q", f.path, dir)
	}
	if f.worktreeAge != 30 || f.ignoredAge != 60 || !f.ignored {
		t.Errorf("thresholds not carried over: %+v", f)
	}

	// A root that happens to be the working directory reads as "this folder".
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	f = newOptionsForm(scan.Config{Mode: domain.ModeRoot, Root: cwd})
	if f.scope != scopeHere {
		t.Errorf("scope = %v, want here when root is the cwd", f.scope)
	}
}

// TestCursorSkipsHiddenFields covers the rows that don't apply to the current
// scope: landing the cursor on one would let the user "change" a setting the
// scan can't act on.
func TestCursorSkipsHiddenFields(t *testing.T) {
	f := newOptionsForm(scan.Config{}) // curated
	if f.visible(fieldPath) || f.visible(fieldWorktreeAge) || f.visible(fieldIgnored) {
		t.Error("curated scope should hide path and walk-only thresholds")
	}
	// Whatever the scope hides, a lap of the cursor only ever stops on rows
	// that are actually on screen.
	f.cursor = fieldScope
	for i := 0; i < int(fieldCount)*2; i++ {
		f.moveCursor(1)
		if !f.visible(f.cursor) {
			t.Fatalf("cursor landed on hidden field %v", f.cursor)
		}
	}
	// Real disk use applies to every scope, so curated is never down to one row.
	if !f.visible(fieldRealSize) {
		t.Error("the real-size toggle should be available in every scope")
	}

	f.scope = scopeHere
	if !f.visible(fieldPath) || !f.visible(fieldWorktreeAge) {
		t.Error("a walking scope should expose path and worktree age")
	}
	// The cruft-age row only matters once cruft scanning is on.
	f.ignored = false
	if f.visible(fieldIgnoredAge) {
		t.Error("cruft age should be hidden while cruft scanning is off")
	}
	f.ignored = true
	if !f.visible(fieldIgnoredAge) {
		t.Error("cruft age should appear once cruft scanning is on")
	}
}

func TestAdjustScopeCyclesAndMovesCursorOffHiddenRow(t *testing.T) {
	f := newOptionsForm(scan.Config{})
	f.scope = scopeHere
	f.cursor = fieldWorktreeAge

	// Back to curated hides the row the cursor is sitting on.
	f.scope = scopeCurated
	f.cursor = fieldScope
	f.adjust(1) // curated -> home
	if f.scope != scopeHome {
		t.Errorf("scope = %v, want home", f.scope)
	}
	f.adjust(-1)
	if f.scope != scopeCurated {
		t.Errorf("scope = %v, want to cycle back to curated", f.scope)
	}
	f.adjust(-1)
	if f.scope != scopeCustom {
		t.Errorf("scope = %v, want to wrap round to custom", f.scope)
	}
}

func TestAdjustAges(t *testing.T) {
	f := newOptionsForm(scan.Config{})
	f.cursor = fieldWorktreeAge
	f.worktreeAge = 14
	f.adjust(1)
	if f.worktreeAge != 15 {
		t.Errorf("worktreeAge = %d, want 15", f.worktreeAge)
	}

	// Stepping down past 1 reaches -1, which means "no age filter".
	f.worktreeAge = 1
	f.adjust(-1)
	if f.worktreeAge != -1 {
		t.Errorf("worktreeAge = %d, want -1 (no filter)", f.worktreeAge)
	}
	f.adjust(-1)
	if f.worktreeAge != -1 {
		t.Errorf("worktreeAge = %d, want to clamp at -1", f.worktreeAge)
	}
	if got := ageLabel(-1); !strings.Contains(got, "all") {
		t.Errorf("ageLabel(-1) = %q, want it to say everything is reported", got)
	}

	f.cursor = fieldIgnored
	f.ignored = false
	f.adjust(1)
	if !f.ignored {
		t.Error("adjust should toggle the cruft switch")
	}
}

func TestFormConfigMapping(t *testing.T) {
	dir := t.TempDir()

	f := newOptionsForm(scan.Config{})
	f.scope = scopeCurated
	f.ignored = true
	cfg, err := f.config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != domain.ModeCurated {
		t.Errorf("Mode = %v, want curated", cfg.Mode)
	}
	// Cruft scanning needs a walk, so curated must not claim to do it.
	if cfg.ScanIgnored {
		t.Error("curated scope should not enable cruft scanning")
	}

	f.scope = scopeCustom
	f.path = dir
	f.ignored = true
	f.worktreeAge = 21
	cfg, err = f.config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != domain.ModeRoot || cfg.Root != dir {
		t.Errorf("Mode/Root = %v/%q, want root/%q", cfg.Mode, cfg.Root, dir)
	}
	if !cfg.ScanIgnored || cfg.WorktreeAgeDays != 21 {
		t.Errorf("thresholds not carried into the config: %+v", cfg)
	}
}

// TestFormConfigExpandsTilde is the TUI half of the bug that made a scan of
// `~/code` silently report nothing: the path has to be expanded before it's
// handed to the scanner.
func TestFormConfigExpandsTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	f := newOptionsForm(scan.Config{})
	f.scope = scopeCustom
	f.path = "~"

	cfg, err := f.config()
	if err != nil {
		t.Fatalf("config() on ~ failed: %v", err)
	}
	if cfg.Root != home {
		t.Errorf("Root = %q, want %q", cfg.Root, home)
	}
	if strings.HasPrefix(cfg.Root, "~") {
		t.Error("tilde survived into the scan root")
	}
}

// TestFormConfigRejectsMissingPath is the other half: a path that isn't there
// used to scan successfully and report nothing, which reads as a clean disk.
func TestFormConfigRejectsMissingPath(t *testing.T) {
	f := newOptionsForm(scan.Config{})
	f.scope = scopeCustom
	f.path = filepath.Join(t.TempDir(), "definitely-not-here")

	if _, err := f.config(); err == nil {
		t.Error("expected an error for a path that does not exist")
	}

	f.path = "   "
	if _, err := f.config(); err == nil {
		t.Error("expected an error for an empty path")
	}

	// A file is not a scannable root either.
	file := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.path = file
	if _, err := f.config(); err == nil {
		t.Error("expected an error when the root is a file")
	}
}

func TestPathEditing(t *testing.T) {
	f := newOptionsForm(scan.Config{})
	f.path = ""
	f.typePath("/tmp")
	f.typePath("/x")
	if f.path != "/tmp/x" {
		t.Errorf("path = %q, want /tmp/x", f.path)
	}
	f.backspacePath()
	if f.path != "/tmp/" {
		t.Errorf("path = %q, want /tmp/", f.path)
	}
	f.path = ""
	f.backspacePath() // must not panic on empty
}

// TestOptionsScreenRenders drives the model the way a user would and checks the
// screen actually shows the controls.
func TestOptionsScreenRenders(t *testing.T) {
	m := New(Options{ScanConfig: scan.Config{}})
	m.width, m.height = 140, 44

	// `o` from the splash opens the options screen.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	m = next.(Model)
	if m.phase != PhaseOptions {
		t.Fatalf("phase = %v, want options after pressing o", m.phase)
	}

	out := m.View()
	for _, want := range []string{"scan options", "scope", "curated"} {
		if !strings.Contains(out, want) {
			t.Errorf("options screen missing %q\n%s", want, out)
		}
	}

	// Right arrow moves the scope on, and the walk-only rows appear with it.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)
	if m.form.scope != scopeHere {
		t.Fatalf("scope = %v, want here after two right presses", m.form.scope)
	}
	out = m.View()
	for _, want := range []string{"path", "stale worktrees", "gitignored cruft"} {
		if !strings.Contains(out, want) {
			t.Errorf("options screen missing %q once a walking scope is picked\n%s", want, out)
		}
	}

	// esc goes back rather than trapping the user on the screen.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.phase != PhaseSplash {
		t.Errorf("phase = %v, want splash after esc", m.phase)
	}
}

func TestOptionsScreenSurfacesBadPath(t *testing.T) {
	m := New(Options{ScanConfig: scan.Config{}})
	m.width, m.height = 140, 44
	m.phase = PhaseOptions
	m.form.scope = scopeCustom
	m.form.path = filepath.Join(t.TempDir(), "nope")

	// Space starts the scan; with an unusable path it must report instead.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	m = next.(Model)
	if m.phase == PhaseScanning {
		t.Fatal("started a scan on a path that does not exist")
	}
	if m.err == "" {
		t.Fatal("expected an error message for a bad path")
	}
	if !strings.Contains(m.View(), "⚠") {
		t.Error("the error should be visible on the options screen")
	}
}

// TestSkipSplashStartsAScan covers a model that showed the scanning dashboard
// while no scan was running, leaving it pinned at zero for ever.
func TestSkipSplashStartsAScan(t *testing.T) {
	dir := t.TempDir()
	m := New(Options{
		SkipSplash: true,
		ScanConfig: scan.Config{Mode: domain.ModeRoot, Root: dir, MinSize: 1024},
	})
	if m.phase != PhaseScanning {
		t.Fatalf("phase = %v, want scanning", m.phase)
	}
	if m.scanCh != nil {
		t.Fatal("the scan should not have started before Init")
	}

	// Init has to schedule the begin, since it cannot mutate the model itself.
	if cmd := m.Init(); cmd == nil {
		t.Fatal("Init returned no commands")
	}
	next, cmd := m.Update(autoBeginMsg{})
	m = next.(Model)
	if m.scanCh == nil {
		t.Error("autoBeginMsg did not start a scan")
	}
	if cmd == nil {
		t.Error("expected a command waiting on scan events")
	}
	if m.scanCancel != nil {
		m.scanCancel()
	}
}

func TestInitialFilterIsApplied(t *testing.T) {
	m := New(Options{InitialFilter: domain.CatGitWorktree})
	if m.filter != domain.CatGitWorktree {
		t.Errorf("filter = %q, want the worktree category", m.filter)
	}
}
