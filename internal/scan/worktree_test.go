package scan

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLinkedWorktree(t *testing.T) {
	tests := []struct {
		name    string
		gitFile string // contents of .git; "" means create a .git directory
		want    bool
	}{
		{
			name:    "linked worktree",
			gitFile: "gitdir: /repo/.git/worktrees/feature\n",
			want:    true,
		},
		{
			name:    "submodule is not a worktree",
			gitFile: "gitdir: /repo/.git/modules/vendor-lib\n",
			want:    false,
		},
		{
			name:    "ordinary checkout has a .git directory",
			gitFile: "",
			want:    false,
		},
		{
			name:    "unrelated file named .git",
			gitFile: "just some text\n",
			want:    false,
		},
		{
			name:    "no trailing newline",
			gitFile: "gitdir: /repo/.git/worktrees/feature",
			want:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gitPath := filepath.Join(dir, ".git")
			if tc.gitFile == "" {
				if err := os.Mkdir(gitPath, 0o755); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(gitPath, []byte(tc.gitFile), 0o644); err != nil {
				t.Fatal(err)
			}

			got, gitDir := linkedWorktree(dir)
			if got != tc.want {
				t.Errorf("linkedWorktree() = %v, want %v", got, tc.want)
			}
			if got && gitDir == "" {
				t.Error("linkedWorktree() reported a worktree but returned no gitdir")
			}
		})
	}
}

func TestLinkedWorktreeRejectsOversizedFile(t *testing.T) {
	dir := t.TempDir()
	big := make([]byte, 5000)
	copy(big, "gitdir: /repo/.git/worktrees/feature")
	if err := os.WriteFile(filepath.Join(dir, ".git"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, _ := linkedWorktree(dir); ok {
		t.Error("expected an oversized .git file to be rejected")
	}
}

func TestParseGitStatus(t *testing.T) {
	tests := []struct {
		name       string
		out        string
		wantDirty  int
		wantAhead  int
		wantBranch string
		wantSafe   bool
	}{
		{
			name: "clean and in sync",
			out: "# branch.oid abc123\n" +
				"# branch.head main\n" +
				"# branch.ab +0 -0\n",
			wantDirty:  0,
			wantAhead:  0,
			wantBranch: "main",
			wantSafe:   true,
		},
		{
			name: "modified and untracked files",
			out: "# branch.head feature/x\n" +
				"# branch.ab +0 -0\n" +
				"1 .M N... 100644 100644 100644 aaa bbb src/main.go\n" +
				"? notes.txt\n",
			wantDirty:  2,
			wantAhead:  0,
			wantBranch: "feature/x",
			wantSafe:   false,
		},
		{
			name: "unpushed commits",
			out: "# branch.head feature/y\n" +
				"# branch.ab +3 -0\n",
			wantDirty:  0,
			wantAhead:  3,
			wantBranch: "feature/y",
			wantSafe:   false,
		},
		{
			name: "behind only is still safe",
			out: "# branch.head main\n" +
				"# branch.ab +0 -7\n",
			wantDirty:  0,
			wantAhead:  0,
			wantBranch: "main",
			wantSafe:   true,
		},
		{
			name:       "no upstream is not safe",
			out:        "# branch.head orphan\n",
			wantDirty:  0,
			wantAhead:  -1,
			wantBranch: "orphan",
			wantSafe:   false,
		},
		{
			name: "unmerged paths count as dirty",
			out: "# branch.head main\n" +
				"# branch.ab +0 -0\n" +
				"u UU N... 100644 100644 100644 100644 aa bb cc conflict.go\n",
			wantDirty:  1,
			wantAhead:  0,
			wantBranch: "main",
			wantSafe:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := parseGitStatus(tc.out)
			if st.Dirty != tc.wantDirty {
				t.Errorf("Dirty = %d, want %d", st.Dirty, tc.wantDirty)
			}
			if st.Ahead != tc.wantAhead {
				t.Errorf("Ahead = %d, want %d", st.Ahead, tc.wantAhead)
			}
			if st.Branch != tc.wantBranch {
				t.Errorf("Branch = %q, want %q", st.Branch, tc.wantBranch)
			}
			if st.Safe() != tc.wantSafe {
				t.Errorf("Safe() = %v, want %v", st.Safe(), tc.wantSafe)
			}
			if !tc.wantSafe && st.Warning() == "" {
				t.Error("an unsafe state must produce a warning")
			}
			if tc.wantSafe && st.Warning() != "" {
				t.Errorf("a safe state must not warn, got %q", st.Warning())
			}
		})
	}
}

func TestGitStateUnreadableIsUnsafe(t *testing.T) {
	st := gitState{OK: false}
	if st.Safe() {
		t.Error("a worktree whose git state could not be read must not be called safe")
	}
	if st.Warning() == "" {
		t.Error("an unreadable git state must warn")
	}
}

// TestContentModTimeIgnoresDirectoryMtime is the regression this whole feature
// exists for: a worktree whose top-level directory was touched recently, but
// whose actual source has been untouched for weeks, must date as old.
func TestContentModTimeIgnoresDirectoryMtime(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-40 * 24 * time.Hour)

	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(srcDir, "main.go")
	if err := os.WriteFile(src, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(src, old, old); err != nil {
		t.Fatal(err)
	}

	// Something bumps the top-level directory's own mtime: a build artifact
	// dropped in, a git operation, an editor's scratch file.
	if err := os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := contentModTime(context.Background(), dir)
	if got.IsZero() {
		t.Fatal("contentModTime found nothing")
	}
	if age := time.Since(got); age < 39*24*time.Hour {
		t.Errorf("dated the worktree %v old, want ~40 days, directory mtime leaked into the signal", age)
	}
}

func TestContentModTimeSkipsRegenerableDirs(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-40 * 24 * time.Hour)

	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(src, old, old); err != nil {
		t.Fatal(err)
	}

	// A fresh `npm install` must not make abandoned work look active.
	nm := filepath.Join(dir, "node_modules", "left-pad")
	if err := os.MkdirAll(nm, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nm, "index.js"), []byte("//\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := contentModTime(context.Background(), dir)
	if age := time.Since(got); age < 39*24*time.Hour {
		t.Errorf("dated the worktree %v old, want ~40 days, node_modules leaked into the signal", age)
	}
}

func TestContentModTimeSkipsGitDir(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-40 * 24 * time.Hour)

	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(src, old, old); err != nil {
		t.Fatal(err)
	}

	// `git status` alone rewrites index files; that isn't the user working.
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "index"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := contentModTime(context.Background(), dir)
	if age := time.Since(got); age < 39*24*time.Hour {
		t.Errorf("dated the worktree %v old, want ~40 days, .git leaked into the signal", age)
	}
}

// TestContentModTimeSkipsGitPointerFile covers the linked-worktree shape
// specifically: there `.git` is a regular file, written when the worktree was
// created and rewritten by git as it works. Counting it dated every worktree
// to whenever git last touched it rather than to the user's last edit.
func TestContentModTimeSkipsGitPointerFile(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-40 * 24 * time.Hour)

	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(src, old, old); err != nil {
		t.Fatal(err)
	}

	pointer := []byte("gitdir: /repo/.git/worktrees/feature\n")
	if err := os.WriteFile(filepath.Join(dir, ".git"), pointer, 0o644); err != nil {
		t.Fatal(err)
	}

	got := contentModTime(context.Background(), dir)
	if age := time.Since(got); age < 39*24*time.Hour {
		t.Errorf("dated the worktree %v old, want ~40 days, the .git pointer file leaked into the signal", age)
	}
}

func TestContentModTimeFindsRecentEdit(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-40 * 24 * time.Hour)

	stale := filepath.Join(dir, "old.go")
	if err := os.WriteFile(stale, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	// A file edited today, buried deep. The directory mtime signal would miss
	// this entirely and call the worktree abandoned.
	deep := filepath.Join(dir, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "today.go"), []byte("package c\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := contentModTime(context.Background(), dir)
	if age := time.Since(got); age > time.Hour {
		t.Errorf("dated the worktree %v old, want ~0, a deep recent edit was missed", age)
	}
}

func TestContentModTimeEmptyDir(t *testing.T) {
	if got := contentModTime(context.Background(), t.TempDir()); !got.IsZero() {
		t.Errorf("expected the zero time for an empty directory, got %v", got)
	}
}

// TestReadGitStateAgainstRealRepo exercises the actual git invocation rather
// than just the parser, so a change in git's porcelain contract is caught.
func TestReadGitStateAgainstRealRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-qm", "first")

	st := readGitState(context.Background(), dir)
	if !st.OK {
		t.Fatal("expected to read git state from a real repo")
	}
	if st.Branch != "main" {
		t.Errorf("Branch = %q, want main", st.Branch)
	}
	if st.Dirty != 0 {
		t.Errorf("Dirty = %d, want 0 on a fresh commit", st.Dirty)
	}
	// No remote configured, so there is no upstream to be in sync with.
	if st.Safe() {
		t.Error("a branch with no upstream must not be reported safe")
	}

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st = readGitState(context.Background(), dir)
	if st.Dirty != 1 {
		t.Errorf("Dirty = %d, want 1 after modifying a tracked file", st.Dirty)
	}
}

func TestIsNoiseFile(t *testing.T) {
	noise := []string{".DS_Store", "Thumbs.db", "build.log", "server.pid", ".tsbuildinfo"}
	for _, n := range noise {
		if !isNoiseFile(n) {
			t.Errorf("isNoiseFile(%q) = false, want true", n)
		}
	}
	real := []string{"main.go", "README.md", "package.json", "logger.ts"}
	for _, n := range real {
		if isNoiseFile(n) {
			t.Errorf("isNoiseFile(%q) = true, want false", n)
		}
	}
}
