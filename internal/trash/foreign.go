package trash

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// needsAdmin reports whether an item can't be moved or removed as the current
// user: it belongs to someone else, or its folder isn't ours to change. Finder
// can't put such an item in our Trash, so it falls back to deleting it
// outright and asks for a password once per file.
func needsAdmin(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return true
	}
	return syscall.Access(filepath.Dir(path), 2 /* W_OK */) != nil
}

// takeForeign deletes everything needing admin rights in one privileged batch,
// so the password is asked for once rather than for every file. These can't
// go to the Trash whichever way they're removed; the macOS prompt says so and
// is the confirmation.
func takeForeign(paths []string, res *Result) []string {
	var foreign, rest []string
	for _, p := range paths {
		if needsAdmin(p) {
			foreign = append(foreign, p)
		} else {
			rest = append(rest, p)
		}
	}
	if len(foreign) == 0 {
		return rest
	}
	cmd := exec.Command("osascript", "-e", adminDeleteScript(foreign))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		err = fmt.Errorf("admin delete: %s", cleanStderr(stderr.String()))
		for _, p := range foreign {
			res.Failed[p] = err
		}
		return rest
	}
	for _, p := range foreign {
		if _, err := os.Lstat(p); err == nil {
			res.Failed[p] = fmt.Errorf("still there after admin delete")
			continue
		}
		res.Trashed = append(res.Trashed, p)
	}
	return rest
}

func adminDeleteScript(paths []string) string {
	quoted := make([]string, len(paths))
	for i, p := range paths {
		quoted[i] = shellQuote(p)
	}
	shell := "/bin/rm -rf -- " + strings.Join(quoted, " ")
	prompt := fmt.Sprintf("springclean wants to permanently delete %d item(s) owned by another user. They can't go to your Trash.", len(paths))
	return fmt.Sprintf("do shell script %s with prompt %s with administrator privileges",
		applescriptString(shell), applescriptString(prompt))
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
