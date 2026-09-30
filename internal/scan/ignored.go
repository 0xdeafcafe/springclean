package scan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/0xdeafcafe/springclean/internal/catalog"
	"github.com/0xdeafcafe/springclean/internal/disk"
	"github.com/0xdeafcafe/springclean/internal/domain"
	"github.com/dustin/go-humanize"
)

// gitEntry finds the `.git` entry in a directory listing contains `.git`, marking it
// as the top of a repository or linked worktree. Either way, it is the level
// that owns a set of ignore rules.
//
// This takes the entries the walk has already read rather than doing its own
// stat: the walk visits millions of directories, and an extra syscall at each
// one is not free.
func gitEntry(entries []disk.Entry) (disk.Entry, bool) {
	for _, e := range entries {
		if e.Name == ".git" {
			return e, true
		}
	}
	return disk.Entry{}, false
}

// listIgnored returns the paths in `repo` that git ignores, relative to the
// repo root, directories carrying a trailing slash.
//
// This is two steps rather than one. `ls-files --directory` collapses an
// untracked directory into a single entry, which is what keeps this affordable
// on a tree with a large node_modules. But it collapses directories that are
// merely untracked as well as ones that are ignored. A nested repository, or
// any folder the user simply hasn't committed, comes back looking exactly like
// a build cache. So every candidate is put back to `check-ignore`, and only
// paths git confirms are matched by an ignore rule survive.
func listIgnored(ctx context.Context, repo string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	out, err := gitOut(ctx, repo, nil, "ls-files",
		"--others", "--ignored", "--exclude-standard", "--directory", "-z")
	if err != nil {
		return nil, err
	}
	candidates := splitNUL(out)
	if len(candidates) == 0 {
		return nil, nil
	}
	return confirmIgnored(ctx, repo, candidates)
}

// confirmIgnored keeps only the paths an ignore rule actually matches.
func confirmIgnored(ctx context.Context, repo string, candidates []string) ([]string, error) {
	stdin := strings.Join(candidates, "\x00") + "\x00"
	out, err := gitOut(ctx, repo, strings.NewReader(stdin),
		"check-ignore", "--stdin", "-z")
	if err != nil {
		// check-ignore exits 1 when nothing matched. That's an answer, not a
		// failure, and it means everything ls-files offered was merely
		// untracked rather than ignored.
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}

	confirmed := map[string]bool{}
	for _, p := range splitNUL(out) {
		confirmed[strings.TrimSuffix(p, "/")] = true
	}
	// Preserve the original entries so trailing slashes (and therefore the
	// directory/file distinction) survive the round trip.
	var kept []string
	for _, c := range candidates {
		if confirmed[strings.TrimSuffix(c, "/")] {
			kept = append(kept, c)
		}
	}
	return kept, nil
}

func gitOut(ctx context.Context, dir string, stdin io.Reader, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Stdin = stdin
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	return cmd.Output()
}

func splitNUL(b []byte) []string {
	var out []string
	for _, p := range strings.Split(string(b), "\x00") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// scanIgnored flags gitignored files and directories that are both large and
// stale. This is the catch-all behind the curated marker list: it finds the
// project-specific junk (data dumps, generated fixtures, stray archives)
// that no hardcoded set of directory names can anticipate.
//
// Entries whose name is already a known marker are left alone so they stay
// categorised as dev cache instead of being double-counted here.
func (s *Scanner) scanIgnored(repo string) {
	if !s.markIgnoredScanned(repo) {
		return
	}
	s.submit(func() {
		rels, err := listIgnored(s.ctx, repo)
		if err != nil {
			// Not a usable repo (bare, corrupt, git missing). Nothing to
			// report, and the ordinary walk still covers this tree.
			return
		}
		for _, rel := range rels {
			clean := strings.TrimSuffix(rel, "/")
			if clean == "" || catalog.Regenerable(filepath.Base(clean)) {
				continue
			}
			if coveredByParent(clean, rels) {
				continue
			}
			s.sizeIgnored(filepath.Join(repo, clean), strings.HasSuffix(rel, "/"))
		}
	})
}

// coveredByParent reports whether `rel` sits inside another ignored entry from
// the same listing. git can report both a directory and paths beneath it;
// emitting both would count those bytes twice.
func coveredByParent(rel string, all []string) bool {
	for _, other := range all {
		if !strings.HasSuffix(other, "/") {
			continue
		}
		if parent := strings.TrimSuffix(other, "/"); parent != rel &&
			strings.HasPrefix(rel, parent+"/") {
			return true
		}
	}
	return false
}

// containsCheckout reports whether `dir` holds a git repository or linked
// worktree within a few levels.
//
// Being gitignored doesn't make a directory disposable: the conventional place
// to keep worktrees is a path the repo ignores, so the whole worktree
// collection can come back from git as a single ignored entry. Collapsing that
// into one suspect would offer every worktree for deletion at once, under a
// size and age computed across all of them. Directories like this are left to
// the ordinary walk, which evaluates each checkout inside on its own terms.
func containsCheckout(dir string, maxDepth int) bool {
	if maxDepth < 0 {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Name() == ".git" {
			return true
		}
	}
	for _, e := range entries {
		if e.IsDir() && !catalog.Regenerable(e.Name()) {
			if containsCheckout(filepath.Join(dir, e.Name()), maxDepth-1) {
				return true
			}
		}
	}
	return false
}

// sizeIgnored measures one ignored path and emits it when it clears both the
// size and staleness bars.
func (s *Scanner) sizeIgnored(path string, isDir bool) {
	s.submit(func() {
		if !s.markSeen(path) {
			return
		}
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return
		}

		var usage disk.Usage
		var touched time.Time
		if isDir && info.IsDir() {
			if containsCheckout(path, 3) {
				return
			}
			usage = s.sumDir(path)
			touched = contentModTime(s.ctx, path)
		} else {
			usage = s.sumFile(path, info)
			touched = info.ModTime()
		}
		if touched.IsZero() {
			touched = info.ModTime()
		}

		if max(usage.Real, usage.Apparent) < catalog.IgnoredCruftMinSize {
			return
		}
		age := time.Since(touched)
		if age < s.ignoredMinAge() {
			return
		}

		sus := domain.Suspect{
			ID:       domain.MakeID(path),
			Path:     path,
			Category: domain.CatIgnoredCruft,
			Reason: fmt.Sprintf("Gitignored %s, untouched for %d days (%s)",
				kindWord(info.IsDir()), int(age.Hours()/24), humanize.Bytes(uint64(usage.Real))),
			IsDir:    info.IsDir(),
			LastUsed: touched,
			// Ignored by git means it isn't source, but it isn't necessarily
			// reproducible either: a dumped database won't come back from a
			// build step. Leave the regenerable claim to the marker list.
			Regenerable: false,
		}
		record(&sus, usage)
		s.emit(sus)
	})
}

func kindWord(isDir bool) string {
	if isDir {
		return "directory"
	}
	return "file"
}

// ignoredMinAge mirrors worktreeMinAge: zero means the catalog default,
// negative disables the filter.
func (s *Scanner) ignoredMinAge() time.Duration {
	switch {
	case s.cfg.IgnoredAgeDays < 0:
		return 0
	case s.cfg.IgnoredAgeDays == 0:
		return catalog.IgnoredCruftAgeDays * 24 * time.Hour
	default:
		return time.Duration(s.cfg.IgnoredAgeDays) * 24 * time.Hour
	}
}

// markIgnoredScanned keeps one `git ls-files` per repository. The walk visits
// every directory in a tree, but only the root has the answer for all of them.
func (s *Scanner) markIgnoredScanned(repo string) bool {
	s.ignoredMu.Lock()
	defer s.ignoredMu.Unlock()
	if s.ignoredScanned == nil {
		s.ignoredScanned = map[string]bool{}
	}
	if s.ignoredScanned[repo] {
		return false
	}
	s.ignoredScanned[repo] = true
	return true
}
