package trash

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrAutomationDenied is returned when macOS blocks osascript from sending
// Apple events to Finder (TCC Automation permission). The caller should
// prompt the user to grant permission or fall back to MoveManyManual.
var ErrAutomationDenied = errors.New("automation permission denied: terminal can't control Finder")

// Result reports the outcome of attempting to trash a batch of paths.
type Result struct {
	Trashed []string
	Failed  map[string]error
	Skipped []string // didn't exist
	Method  string   // "finder" or "manual"
}

// MoveMany sends each path to the macOS Trash via Finder (preserves Put Back).
// Missing paths are filtered out up-front (cheap Lstat) so we never invoke
// osascript for them. Remaining paths are tried as a single batch first, with
// per-path fallback if the batch errors. If macOS denies the Automation
// permission, returns ErrAutomationDenied via Result.Failed[""] so the caller
// can prompt the user.
func MoveMany(paths []string) Result {
	res := Result{Failed: map[string]error{}, Method: "finder"}
	if len(paths) == 0 {
		return res
	}

	var existing []string
	for _, p := range paths {
		if _, err := os.Lstat(p); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				res.Skipped = append(res.Skipped, p)
				continue
			}
			res.Failed[p] = fmt.Errorf("can't stat: %w", err)
			continue
		}
		existing = append(existing, p)
	}
	if len(existing) == 0 {
		return res
	}

	if err := runFinderDelete(existing); err == nil {
		res.Trashed = append(res.Trashed, existing...)
		return res
	} else if errors.Is(err, ErrAutomationDenied) {
		for _, p := range existing {
			res.Failed[p] = ErrAutomationDenied
		}
		return res
	}

	for _, p := range existing {
		if err := runFinderDelete([]string{p}); err != nil {
			if errors.Is(err, ErrAutomationDenied) {
				res.Failed[p] = ErrAutomationDenied
				continue
			}
			if isMissingPathErr(err) {
				res.Skipped = append(res.Skipped, p)
			} else {
				res.Failed[p] = err
			}
		} else {
			res.Trashed = append(res.Trashed, p)
		}
	}
	return res
}

// MoveManyManual moves paths into ~/.Trash via a plain rename (or copy+delete
// across volumes). No osascript, no Finder — and no "Put Back" metadata. Use
// as a fallback when ErrAutomationDenied is hit and the user opts in.
func MoveManyManual(paths []string) Result {
	res := Result{Failed: map[string]error{}, Method: "manual"}
	if len(paths) == 0 {
		return res
	}
	home, err := os.UserHomeDir()
	if err != nil {
		for _, p := range paths {
			res.Failed[p] = err
		}
		return res
	}
	trashDir := filepath.Join(home, ".Trash")
	if err := os.MkdirAll(trashDir, 0o755); err != nil {
		for _, p := range paths {
			res.Failed[p] = err
		}
		return res
	}

	for _, p := range paths {
		if _, err := os.Lstat(p); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				res.Skipped = append(res.Skipped, p)
				continue
			}
			res.Failed[p] = err
			continue
		}
		dest := uniqueTrashDest(trashDir, filepath.Base(p))
		if err := moveTo(p, dest); err != nil {
			res.Failed[p] = err
			continue
		}
		res.Trashed = append(res.Trashed, p)
	}
	return res
}

// DeleteMany removes paths outright, without the Trash.
//
// The Trash is the right default because it is reversible, but it is a bad fit
// for a directory holding a hundred thousand small files: Finder moves them,
// then makes you watch it delete them one by one when you empty the bin. For a
// node_modules that is going to be reinstalled anyway, the round trip is all
// cost and no benefit. Nothing here is recoverable, so callers must confirm
// first.
func DeleteMany(paths []string) Result {
	res := Result{Failed: map[string]error{}, Method: "deleted"}
	for _, p := range paths {
		if _, err := os.Lstat(p); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				res.Skipped = append(res.Skipped, p)
				continue
			}
			res.Failed[p] = err
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			res.Failed[p] = err
			continue
		}
		res.Trashed = append(res.Trashed, p)
	}
	return res
}

func uniqueTrashDest(trashDir, name string) string {
	dest := filepath.Join(trashDir, name)
	if _, err := os.Lstat(dest); err != nil {
		return dest
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	ts := time.Now().Format("2006-01-02 15-04-05")
	return filepath.Join(trashDir, fmt.Sprintf("%s (%s)%s", stem, ts, ext))
}

func moveTo(src, dest string) error {
	if err := os.Rename(src, dest); err == nil {
		return nil
	}
	// Rename failed (likely cross-volume EXDEV) — fall back to copy+remove.
	return copyThenRemove(src, dest)
}

func copyThenRemove(src, dest string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := copyDir(src, dest); err != nil {
			return err
		}
	} else {
		if err := copyFile(src, dest); err != nil {
			return err
		}
	}
	return os.RemoveAll(src)
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func copyDir(src, dest string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dest, info.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dest, e.Name())
		if e.IsDir() {
			if err := copyDir(s, d); err != nil {
				return err
			}
		} else if e.Type()&os.ModeSymlink != 0 {
			tgt, err := os.Readlink(s)
			if err != nil {
				continue
			}
			_ = os.Symlink(tgt, d)
		} else {
			if err := copyFile(s, d); err != nil {
				return err
			}
		}
	}
	return nil
}

func runFinderDelete(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	script := buildScript(paths)
	cmd := exec.Command("osascript", "-e", script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		stderrStr := cleanStderr(stderr.String())
		if isAutomationDeniedErr(stderrStr) {
			return ErrAutomationDenied
		}
		return fmt.Errorf("osascript: %w: %s", err, stderrStr)
	}
	return nil
}

func isAutomationDeniedErr(stderr string) bool {
	s := strings.ToLower(stderr)
	return strings.Contains(s, "(-1743)") ||
		strings.Contains(s, "not authori") || // "not authorized" / "not authorised"
		strings.Contains(s, "not allowed to send apple events")
}

// IsAutomationDenied reports whether an error from trashing represents a TCC
// Automation refusal — whether wrapped or returned as ErrAutomationDenied.
// Callers should prefer this over errors.Is when an upstream layer may have
// wrapped the error in a fmt.Errorf.
func IsAutomationDenied(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrAutomationDenied) {
		return true
	}
	return isAutomationDeniedErr(err.Error())
}

func buildScript(paths []string) string {
	if len(paths) == 1 {
		return fmt.Sprintf(`tell application "Finder" to delete (POSIX file %s as alias)`, applescriptString(paths[0]))
	}
	var sb strings.Builder
	sb.WriteString(`tell application "Finder"` + "\n")
	sb.WriteString("\tset itemsToDelete to {")
	for i, p := range paths {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(fmt.Sprintf("(POSIX file %s as alias)", applescriptString(p)))
	}
	sb.WriteString("}\n")
	sb.WriteString("\trepeat with theItem in itemsToDelete\n")
	sb.WriteString("\t\tdelete theItem\n")
	sb.WriteString("\tend repeat\n")
	sb.WriteString("end tell")
	return sb.String()
}

// applescriptString quotes a string as an AppleScript literal. AppleScript
// only escapes backslash and double-quote inside string literals — control
// characters like \n are not honoured, so we leave them as-is (paths can't
// legally contain newlines on macOS).
func applescriptString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

func cleanStderr(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	// Trim verbose "execution error:" framing for readability.
	if i := strings.Index(s, "execution error:"); i >= 0 {
		s = strings.TrimSpace(s[i+len("execution error:"):])
	}
	return s
}

func isMissingPathErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	patterns := []string{
		"doesn't exist",
		"file not found",
		"no such file",
		"can't get file",
		"wasn't found",
		"(-43)",   // fnfErr — file not found
		"(-1700)", // errAECoercionFail — common when alias target is gone
	}
	for _, p := range patterns {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}
