package gitwt

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/0xdeafcafe/springclean/internal/domain"
)

func TestMainRepoFromGitDir(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"/Users/x/Projects/app/.git/worktrees/feature", "/Users/x/Projects/app"},
		{"/repo/.git/worktrees/nested/name", "/repo"},
		{"/repo/.git/modules/vendor", ""},
		{"/not/a/worktree", ""},
		{"", ""},
		// A repo that happens to live under a directory called "worktrees".
		{"/Users/x/worktrees/app/.git/worktrees/feat", "/Users/x/worktrees/app"},
	}
	for _, tc := range tests {
		if got := domain.MainRepoFromGitDir(tc.in); got != tc.want {
			t.Errorf("MainRepoFromGitDir(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestReposToPrune(t *testing.T) {
	suspects := []domain.Suspect{
		// Two worktrees from the same repo collapse to one prune.
		{Marked: true, Category: domain.CatGitWorktree, GitDir: "/a/.git/worktrees/one"},
		{Marked: true, Category: domain.CatGitWorktree, GitDir: "/a/.git/worktrees/two"},
		{Marked: true, Category: domain.CatGitWorktree, GitDir: "/b/.git/worktrees/three"},
		// Unmarked worktrees aren't being removed, so nothing to prune.
		{Marked: false, Category: domain.CatGitWorktree, GitDir: "/c/.git/worktrees/four"},
		// Other categories never carry a registration.
		{Marked: true, Category: domain.CatDevCache, GitDir: ""},
		// A worktree we couldn't resolve an admin dir for.
		{Marked: true, Category: domain.CatGitWorktree, GitDir: ""},
	}

	got := ReposToPrune(suspects)
	want := []string{"/a", "/b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReposToPrune() = %v, want %v", got, want)
	}
}

func TestReposToPruneEmpty(t *testing.T) {
	if got := ReposToPrune(nil); len(got) != 0 {
		t.Errorf("expected no repos, got %v", got)
	}
}

// TestPruneRemovesRegistration proves the thing that motivates this package:
// trashing a worktree directory leaves git still listing it, and prune is what
// actually clears it.
func TestPruneRemovesRegistration(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
	)
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}

	run(repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(repo, "add", "a.txt")
	run(repo, "commit", "-qm", "first")

	wt := filepath.Join(t.TempDir(), "feature")
	run(repo, "worktree", "add", "-q", "-b", "feature", wt)

	if !strings.Contains(run(repo, "worktree", "list"), wt) {
		t.Fatal("expected the new worktree to be registered")
	}

	// Simulate springclean moving the directory to the Trash.
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(run(repo, "worktree", "list"), "prunable") {
		t.Fatal("expected git to still list the removed worktree as prunable")
	}

	if failed := Prune(context.Background(), []string{repo}); len(failed) != 0 {
		t.Fatalf("Prune reported failures: %v", failed)
	}

	if strings.Contains(run(repo, "worktree", "list"), wt) {
		t.Error("worktree still registered after prune")
	}
}

func TestPruneReportsFailureForNonRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	failed := Prune(context.Background(), []string{dir})
	if len(failed) != 1 {
		t.Errorf("expected 1 failure pruning a non-repo, got %d", len(failed))
	}
}
