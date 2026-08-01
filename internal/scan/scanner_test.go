package scan

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xdeafcafe/springclean/internal/domain"
)

// gitEnv keeps test repos free of the user's identity and global config.
func gitEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

// writeSized creates a file of at least n bytes so it clears the scanner's
// minimum-size floor.
func writeSized(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ageFile(t *testing.T, path string, d time.Duration) {
	t.Helper()
	ts := time.Now().Add(-d)
	if err := os.Chtimes(path, ts, ts); err != nil {
		t.Fatal(err)
	}
}

// runScan collects every suspect from a scan of root.
func runScan(t *testing.T, cfg Config) []domain.Suspect {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var got []domain.Suspect
	for ev := range New(cfg).Run(ctx) {
		if se, ok := ev.(SuspectEvent); ok {
			got = append(got, se.S)
		}
	}
	return got
}

func findByCategory(suspects []domain.Suspect, c domain.Category) []domain.Suspect {
	var out []domain.Suspect
	for _, s := range suspects {
		if s.Category == c {
			out = append(out, s)
		}
	}
	return out
}

// setupRepoWithWorktree builds a real repo plus one linked worktree, and
// returns the repo root, the worktree path, and the parent holding both.
func setupRepoWithWorktree(t *testing.T, wtName string) (repo, wt, parent string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	parent = t.TempDir()
	repo = filepath.Join(parent, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q", "-b", "main")
	writeSized(t, filepath.Join(repo, "a.txt"), 16)
	git(t, repo, "add", "a.txt")
	git(t, repo, "commit", "-qm", "first")

	wt = filepath.Join(parent, wtName)
	git(t, repo, "worktree", "add", "-q", "-b", wtName, wt)
	return repo, wt, parent
}

func TestScanEmitsStaleWorktree(t *testing.T) {
	_, wt, parent := setupRepoWithWorktree(t, "stale")

	src := filepath.Join(wt, "big.bin")
	writeSized(t, src, 2*1024*1024)
	ageFile(t, src, 60*24*time.Hour)
	ageFile(t, filepath.Join(wt, "a.txt"), 60*24*time.Hour)

	got := runScan(t, Config{Mode: domain.ModeRoot, Root: parent, MinSize: 1024})
	wts := findByCategory(got, domain.CatGitWorktree)
	if len(wts) != 1 {
		t.Fatalf("expected 1 worktree suspect, got %d (%+v)", len(wts), wts)
	}
	s := wts[0]
	if s.Path != wt {
		t.Errorf("Path = %q, want %q", s.Path, wt)
	}
	if s.GitDir == "" {
		t.Error("GitDir must be recorded so apply can prune the registration")
	}
	if domain.MainRepoFromGitDir(s.GitDir) == "" {
		t.Errorf("GitDir %q does not resolve back to a repo", s.GitDir)
	}
	if s.Size < 2*1024*1024 {
		t.Errorf("Size = %d, want at least 2 MiB", s.Size)
	}
	if age := time.Since(s.LastUsed); age < 59*24*time.Hour {
		t.Errorf("LastUsed is %v old, want ~60 days", age)
	}
	// The branch has no upstream, so removing it would strand the commit.
	if s.Warning == "" {
		t.Error("expected a warning for a branch with no upstream")
	}
}

// TestScanSkipsFreshWorktree covers the case the old directory-mtime signal got
// backwards: a worktree being actively worked in must not be offered up for
// deletion, and must still have its caches scanned.
func TestScanSkipsFreshWorktree(t *testing.T) {
	_, wt, parent := setupRepoWithWorktree(t, "fresh")

	// Edited just now, buried deep enough that the directory mtime is stale.
	writeSized(t, filepath.Join(wt, "src", "deep", "today.go"), 32)
	// A cache inside it that the walk should still find.
	writeSized(t, filepath.Join(wt, "node_modules", "pkg", "blob.bin"), 2*1024*1024)

	got := runScan(t, Config{Mode: domain.ModeRoot, Root: parent, MinSize: 1024})

	if wts := findByCategory(got, domain.CatGitWorktree); len(wts) != 0 {
		t.Errorf("a worktree touched today must not be flagged, got %+v", wts)
	}
	var foundCache bool
	for _, s := range findByCategory(got, domain.CatDevCache) {
		if filepath.Base(s.Path) == "node_modules" {
			foundCache = true
		}
	}
	if !foundCache {
		t.Error("node_modules inside a fresh worktree should still be found")
	}
}

func TestWorktreeAgeThresholdIsConfigurable(t *testing.T) {
	_, wt, parent := setupRepoWithWorktree(t, "week-old")

	src := filepath.Join(wt, "big.bin")
	writeSized(t, src, 2*1024*1024)
	ageFile(t, src, 7*24*time.Hour)
	ageFile(t, filepath.Join(wt, "a.txt"), 7*24*time.Hour)

	// Default threshold is 14 days, so a 7-day-old worktree is not stale.
	got := runScan(t, Config{Mode: domain.ModeRoot, Root: parent, MinSize: 1024})
	if wts := findByCategory(got, domain.CatGitWorktree); len(wts) != 0 {
		t.Errorf("7-day-old worktree flagged under the 14-day default: %+v", wts)
	}

	// Lowering the bar to 3 days brings it in.
	got = runScan(t, Config{Mode: domain.ModeRoot, Root: parent, MinSize: 1024, WorktreeAgeDays: 3})
	if wts := findByCategory(got, domain.CatGitWorktree); len(wts) != 1 {
		t.Errorf("expected the worktree at a 3-day threshold, got %d", len(wts))
	}

	// A negative threshold disables the filter entirely.
	got = runScan(t, Config{Mode: domain.ModeRoot, Root: parent, MinSize: 1024, WorktreeAgeDays: -1})
	if wts := findByCategory(got, domain.CatGitWorktree); len(wts) != 1 {
		t.Errorf("expected the worktree with the filter disabled, got %d", len(wts))
	}
}

func TestScanFindsIgnoredCruft(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"),
		[]byte("dumps/\n*.sqlite\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".gitignore")
	git(t, repo, "commit", "-qm", "ignore rules")

	// Gitignored, big, and old: exactly the thing no hardcoded name list finds.
	dump := filepath.Join(repo, "dumps", "prod.dump")
	writeSized(t, dump, 60*1024*1024)
	ageFile(t, dump, 90*24*time.Hour)
	ageFile(t, filepath.Join(repo, "dumps"), 90*24*time.Hour)

	// Gitignored and old, but too small to be worth reporting.
	small := filepath.Join(repo, "small.sqlite")
	writeSized(t, small, 1024)
	ageFile(t, small, 90*24*time.Hour)

	// Gitignored and big, but touched today.
	fresh := filepath.Join(repo, "fresh.sqlite")
	writeSized(t, fresh, 60*1024*1024)

	cfg := Config{Mode: domain.ModeRoot, Root: parent, MinSize: 1024, ScanIgnored: true}
	got := runScan(t, cfg)

	cruft := findByCategory(got, domain.CatIgnoredCruft)
	var paths []string
	for _, s := range cruft {
		paths = append(paths, s.Path)
	}
	if len(cruft) != 1 {
		t.Fatalf("expected exactly 1 ignored-cruft suspect, got %d: %v", len(cruft), paths)
	}
	if want := filepath.Join(repo, "dumps"); cruft[0].Path != want {
		t.Errorf("Path = %q, want %q", cruft[0].Path, want)
	}
	if !cruft[0].IsDir {
		t.Error("expected the collapsed directory entry, not individual files")
	}
}

func TestIgnoredScanIsOptIn(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("dumps/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".gitignore")
	git(t, repo, "commit", "-qm", "ignore rules")

	dump := filepath.Join(repo, "dumps", "prod.dump")
	writeSized(t, dump, 60*1024*1024)
	ageFile(t, dump, 90*24*time.Hour)

	got := runScan(t, Config{Mode: domain.ModeRoot, Root: parent, MinSize: 1024})
	if cruft := findByCategory(got, domain.CatIgnoredCruft); len(cruft) != 0 {
		t.Errorf("ignored scanning must be opt-in, got %+v", cruft)
	}
}

// TestIgnoredCruftSkipsWorktreeContainers is the guard against the worst thing
// this scanner could do. Keeping worktrees in a gitignored directory is the
// normal convention, so git reports the whole collection as one ignored entry.
// Flagging that would offer every worktree in it for deletion as a single item.
func TestIgnoredCruftSkipsWorktreeContainers(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"),
		[]byte(".worktrees/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".gitignore")
	git(t, repo, "commit", "-qm", "ignore rules")

	// A worktree living inside the ignored directory, as is conventional.
	wt := filepath.Join(repo, ".worktrees", "feature")
	git(t, repo, "worktree", "add", "-q", "-b", "feature", wt)
	blob := filepath.Join(wt, "big.bin")
	writeSized(t, blob, 60*1024*1024)
	ageFile(t, blob, 90*24*time.Hour)
	ageFile(t, filepath.Join(wt, ".gitignore"), 90*24*time.Hour)

	got := runScan(t, Config{Mode: domain.ModeRoot, Root: parent, MinSize: 1024, ScanIgnored: true})

	container := filepath.Join(repo, ".worktrees")
	for _, s := range findByCategory(got, domain.CatIgnoredCruft) {
		if s.Path == container {
			t.Fatalf("flagged the worktree container %q as a single disposable blob", container)
		}
	}
	// The worktree inside is still evaluated on its own terms.
	wts := findByCategory(got, domain.CatGitWorktree)
	if len(wts) != 1 || wts[0].Path != wt {
		t.Errorf("expected the nested worktree to be judged individually, got %+v", wts)
	}
}

// TestIgnoredCruftSkipsMerelyUntrackedDirs pins a way this scanner could
// destroy real work. `git ls-files --directory` collapses any untracked
// directory into one entry, whether an ignore rule matches it or not, so a
// folder of source the user simply hasn't committed comes back looking
// identical to a build cache. Only paths git confirms are ignored may be
// flagged.
func TestIgnoredCruftSkipsMerelyUntrackedDirs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("cache/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".gitignore")
	git(t, repo, "commit", "-qm", "ignore rules")

	// Uncommitted source. No ignore rule matches it, but it has no tracked
	// files either, so ls-files --directory reports the whole directory.
	src := filepath.Join(repo, "uncommitted-service", "main.go")
	writeSized(t, src, 60*1024*1024)
	ageFile(t, src, 90*24*time.Hour)
	ageFile(t, filepath.Join(repo, "uncommitted-service"), 90*24*time.Hour)

	// Genuinely ignored, for contrast.
	cache := filepath.Join(repo, "cache", "blob.bin")
	writeSized(t, cache, 60*1024*1024)
	ageFile(t, cache, 90*24*time.Hour)
	ageFile(t, filepath.Join(repo, "cache"), 90*24*time.Hour)

	got := runScan(t, Config{Mode: domain.ModeRoot, Root: parent, MinSize: 1024, ScanIgnored: true})

	var paths []string
	for _, s := range findByCategory(got, domain.CatIgnoredCruft) {
		paths = append(paths, s.Path)
		if filepath.Base(s.Path) == "uncommitted-service" {
			t.Errorf("flagged uncommitted source %q as disposable cruft", s.Path)
		}
	}
	if len(paths) != 1 || filepath.Base(paths[0]) != "cache" {
		t.Errorf("expected only the ignored cache directory, got %v", paths)
	}
}

func TestCoveredByParent(t *testing.T) {
	all := []string{"bin/", "bin/haven", "dumps/", "notes.txt", "a/b/"}
	tests := []struct {
		rel  string
		want bool
	}{
		{"bin/haven", true},     // inside bin/
		{"bin", false},          // is the parent itself
		{"dumps", false},        // no enclosing entry
		{"notes.txt", false},    // a plain file
		{"a/b", false},          // is the parent itself
		{"a/b/c.bin", true},     // inside a/b/
		{"binary/thing", false}, // prefix overlap without a path boundary
	}
	for _, tc := range tests {
		if got := coveredByParent(tc.rel, all); got != tc.want {
			t.Errorf("coveredByParent(%q) = %v, want %v", tc.rel, got, tc.want)
		}
	}
}

func TestContainsCheckout(t *testing.T) {
	root := t.TempDir()

	// A plain tree of files is not a checkout container.
	plain := filepath.Join(root, "plain")
	writeSized(t, filepath.Join(plain, "a", "b", "file.bin"), 16)
	if containsCheckout(plain, 3) {
		t.Error("a plain directory tree was reported as containing a checkout")
	}

	// One with a nested repo is.
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(filepath.Join(nested, "inner", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !containsCheckout(nested, 3) {
		t.Error("failed to spot a nested checkout")
	}

	// Beyond the depth limit it stops looking, so the caller stays cheap.
	deep := filepath.Join(root, "deep")
	if err := os.MkdirAll(filepath.Join(deep, "a", "b", "c", "d", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if containsCheckout(deep, 1) {
		t.Error("searched past the depth limit")
	}
}

// TestIgnoredCruftDoesNotDoubleCountKnownMarkers guards the overlap between the
// curated marker list and git's ignore rules: node_modules is both, and must be
// reported once, as dev cache.
func TestIgnoredCruftDoesNotDoubleCountKnownMarkers(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"),
		[]byte("node_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".gitignore")
	git(t, repo, "commit", "-qm", "ignore rules")

	blob := filepath.Join(repo, "node_modules", "pkg", "blob.bin")
	writeSized(t, blob, 60*1024*1024)
	ageFile(t, blob, 90*24*time.Hour)

	got := runScan(t, Config{Mode: domain.ModeRoot, Root: parent, MinSize: 1024, ScanIgnored: true})

	var nmCount int
	for _, s := range got {
		if filepath.Base(s.Path) == "node_modules" {
			nmCount++
			if s.Category != domain.CatDevCache {
				t.Errorf("node_modules categorised as %s, want dev cache", s.Category)
			}
		}
	}
	if nmCount != 1 {
		t.Errorf("node_modules reported %d times, want exactly 1", nmCount)
	}
}
