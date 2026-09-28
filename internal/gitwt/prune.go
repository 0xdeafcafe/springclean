// Package gitwt handles the git-side bookkeeping that outlives a trashed
// worktree.
package gitwt

import (
	"context"
	"os"
	"os/exec"
	"sort"
	"time"

	"github.com/0xdeafcafe/springclean/internal/domain"
)

// ReposToPrune returns the distinct repositories owning the given suspects,
// for suspects that are git worktrees carrying a known admin directory.
//
// Removing a linked worktree's directory doesn't deregister it: git keeps the
// `.git/worktrees/<name>` entry and goes on listing the worktree as present
// but prunable. Pruning is what actually finishes the removal.
func ReposToPrune(suspects []domain.Suspect) []string {
	seen := map[string]bool{}
	for _, s := range suspects {
		if !s.Marked || s.Category != domain.CatGitWorktree || s.GitDir == "" {
			continue
		}
		if repo := domain.MainRepoFromGitDir(s.GitDir); repo != "" {
			seen[repo] = true
		}
	}
	out := make([]string, 0, len(seen))
	for repo := range seen {
		out = append(out, repo)
	}
	sort.Strings(out)
	return out
}

// Prune runs `git worktree prune` in each repo, returning the repos where it
// failed. A failure is not fatal to an apply: the files are already in the
// Trash, and a stale registration is cosmetic. `git worktree prune` by hand
// clears it later.
func Prune(ctx context.Context, repos []string) map[string]error {
	failed := map[string]error{}
	for _, repo := range repos {
		if err := pruneOne(ctx, repo); err != nil {
			failed[repo] = err
		}
	}
	return failed
}

func pruneOne(ctx context.Context, repo string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "worktree", "prune")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	return cmd.Run()
}
