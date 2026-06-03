package scan

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/0xdeafcafe/springclean/internal/catalog"
	"github.com/0xdeafcafe/springclean/internal/domain"
)

func (s *Scanner) detectUnusedApps() {
	s.submit(func() {
		apps := listApps()
		for _, app := range apps {
			s.checkApp(app)
		}
	})
}

func listApps() []string {
	dirs := []string{"/Applications"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "Applications"))
	}
	var out []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".app") {
				continue
			}
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

func (s *Scanner) checkApp(appPath string) {
	lastUsed, ok := mdlsLastUsed(appPath)
	if !ok {
		return
	}
	age := time.Since(lastUsed)
	if age < catalog.UnusedAppThresholdDays*24*time.Hour {
		return
	}

	info, err := os.Lstat(appPath)
	if err != nil {
		return
	}

	size := s.sumDir(appPath)
	if size < s.cfg.MinSize {
		return
	}

	days := int(age.Hours() / 24)
	s.emit(domain.Suspect{
		ID:          domain.MakeID(appPath),
		Path:        appPath,
		Size:        size,
		Category:    domain.CatUnusedApp,
		Reason:      fmt.Sprintf("Not opened in %d days", days),
		IsDir:       info.IsDir(),
		LastUsed:    lastUsed,
		Regenerable: false,
	})
}

// mdlsLastUsed reads kMDItemLastUsedDate via the macOS `mdls` tool.
// Returns (time, true) if a date was found; (zero, false) otherwise.
func mdlsLastUsed(path string) (time.Time, bool) {
	cmd := exec.Command("mdls", "-name", "kMDItemLastUsedDate", "-raw", path)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return time.Time{}, false
	}
	raw := strings.TrimSpace(stdout.String())
	if raw == "" || raw == "(null)" {
		return time.Time{}, false
	}
	// mdls -raw returns e.g. "2024-09-15 14:32:01 +0000"
	t, err := time.Parse("2006-01-02 15:04:05 -0700", raw)
	if err != nil {
		// Fall back to scanning for a date prefix.
		scan := bufio.NewScanner(strings.NewReader(raw))
		for scan.Scan() {
			line := strings.TrimSpace(scan.Text())
			if t2, err := time.Parse("2006-01-02 15:04:05 -0700", line); err == nil {
				return t2, true
			}
		}
		return time.Time{}, false
	}
	return t, true
}
