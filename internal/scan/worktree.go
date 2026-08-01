package scan

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/0xdeafcafe/springclean/internal/catalog"
)

// linkedWorktree inspects `dir` for a `.git` file (regular file, not directory)
// whose contents identify it as a git linked worktree — one created via
// `git worktree add` — as opposed to a submodule.
//
// A linked worktree's `.git` file contains: `gitdir: <main>/.git/worktrees/<name>`
// A submodule's `.git` file contains:        `gitdir: <parent>/.git/modules/<name>`
//
// Returns (true, gitdir-path) only for linked worktrees.
func linkedWorktree(dir string) (bool, string) {
	gitPath := filepath.Join(dir, ".git")
	info, err := os.Lstat(gitPath)
	if err != nil {
		return false, ""
	}
	if info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, ""
	}
	// `.git` files are tiny — a one-line `gitdir: ...` pointer. Cap the read
	// to avoid pulling in anything weird that happens to share the name.
	if info.Size() > 4096 {
		return false, ""
	}
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return false, ""
	}
	line := strings.TrimSpace(string(data))
	const prefix = "gitdir:"
	if !strings.HasPrefix(line, prefix) {
		return false, ""
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	// Distinguish linked worktrees from submodules: only linked worktrees live
	// under `.git/worktrees/`.
	if !strings.Contains(filepath.ToSlash(gitdir), "/worktrees/") {
		return false, ""
	}
	return true, gitdir
}

// gitState summarises whether a worktree holds work that isn't safely stored
// anywhere else. Trashing a worktree with either is destructive.
type gitState struct {
	Branch string
	// Dirty counts entries reported by `git status --porcelain`: modified,
	// staged, and untracked-but-not-ignored files.
	Dirty int
	// Ahead counts commits on the branch that no remote has. -1 when the
	// branch has no upstream at all, which is itself a reason for caution.
	Ahead int
	// OK is false when git couldn't be consulted; callers must then assume
	// the worktree is unsafe rather than assuming it's clean.
	OK bool
}

// Safe reports whether the worktree can be removed without losing work.
func (g gitState) Safe() bool {
	return g.OK && g.Dirty == 0 && g.Ahead == 0
}

// Warning renders the unsafe-state summary, or "" when there's nothing to lose.
func (g gitState) Warning() string {
	if !g.OK {
		return "could not read git status, check by hand before trashing"
	}
	var parts []string
	if g.Dirty > 0 {
		parts = append(parts, plural(g.Dirty, "uncommitted change", "uncommitted changes"))
	}
	switch {
	case g.Ahead < 0:
		parts = append(parts, "branch has no upstream")
	case g.Ahead > 0:
		parts = append(parts, plural(g.Ahead, "unpushed commit", "unpushed commits"))
	}
	return strings.Join(parts, ", ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// readGitState runs `git status` in a worktree to find unsaved work. Git is
// given a deadline because status on a large repo with a cold index can stall,
// and a scan must not hang on one bad worktree.
func readGitState(ctx context.Context, dir string) gitState {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	// --porcelain=v2 --branch gives both the file list and the ahead/behind
	// header in one invocation. Ignored files are excluded by default, which
	// is what we want: build output doesn't make a worktree unsafe to remove.
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain=v2", "--branch")
	cmd.Dir = dir
	// Keep the user's git config/hooks from prompting or writing to the tree.
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		return gitState{OK: false}
	}
	return parseGitStatus(string(out))
}

// parseGitStatus reads `git status --porcelain=v2 --branch` output.
//
// Header lines start with "# branch.<field> <value>"; the ab field carries
// "+<ahead> -<behind>". Everything else is a changed-path record: "1"/"2" for
// tracked changes, "u" for unmerged, "?" for untracked.
func parseGitStatus(out string) gitState {
	st := gitState{OK: true, Ahead: -1}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# branch.head ") {
			st.Branch = strings.TrimPrefix(line, "# branch.head ")
			continue
		}
		if strings.HasPrefix(line, "# branch.ab ") {
			fields := strings.Fields(strings.TrimPrefix(line, "# branch.ab "))
			if len(fields) > 0 {
				if n, err := strconv.Atoi(strings.TrimPrefix(fields[0], "+")); err == nil {
					st.Ahead = n
				}
			}
			continue
		}
		if strings.HasPrefix(line, "# ") {
			continue
		}
		switch line[0] {
		case '1', '2', 'u', '?':
			st.Dirty++
		}
	}
	return st
}

// contentModTime returns the newest modification time among files that
// represent actual work in `root`.
//
// The worktree directory's own mtime is not a usable signal: a directory's
// mtime only moves when a direct child is added or removed, so editing a file
// three levels down never touches it, while an incidental write at the top
// level makes an abandoned worktree look active. Dependency and build
// directories are skipped for the same reason: reinstalling node_modules
// shouldn't reset the clock on work nobody has looked at in a month.
//
// Returns the zero time when nothing qualifying was found.
func contentModTime(ctx context.Context, root string) time.Time {
	var newest time.Time
	var walk func(string, int)
	walk = func(dir string, depth int) {
		if ctx.Err() != nil || depth > 24 {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			name := e.Name()
			// `.git` is a directory in an ordinary checkout and a pointer file
			// in a linked worktree. Both are git's bookkeeping, and neither
			// says anything about when a human last worked here.
			if name == ".git" {
				continue
			}
			if e.IsDir() {
				if catalog.Regenerable(name) {
					continue
				}
				walk(filepath.Join(dir, name), depth+1)
				continue
			}
			if isNoiseFile(name) {
				continue
			}
			info, err := e.Info()
			if err != nil || info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			if mt := info.ModTime(); mt.After(newest) {
				newest = mt
			}
		}
	}
	walk(root, 0)
	return newest
}

// isNoiseFile matches files the OS and tooling rewrite on their own. They say
// nothing about whether a human touched the worktree.
func isNoiseFile(name string) bool {
	switch name {
	case ".DS_Store", "Thumbs.db", ".tsbuildinfo":
		return true
	}
	return strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".pid")
}
