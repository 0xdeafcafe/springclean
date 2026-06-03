package scan

import (
	"os"
	"path/filepath"
	"strings"
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
