// Package simctl lists and removes Xcode simulator runtimes. They live on
// their own disk images under SIP-protected paths, so a file walk never sees
// them and the Trash can't take them; only simctl can.
package simctl

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Runtime is one entry from `xcrun simctl runtime list -j`.
type Runtime struct {
	Identifier         string    `json:"identifier"`
	Path               string    `json:"path"`
	SizeBytes          int64     `json:"sizeBytes"`
	Version            string    `json:"version"`
	PlatformIdentifier string    `json:"platformIdentifier"`
	Deletable          bool      `json:"deletable"`
	LastUsedAt         time.Time `json:"lastUsedAt"`
}

// List returns installed runtimes, or nothing when Xcode isn't installed.
func List(ctx context.Context) []Runtime {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "xcrun", "simctl", "runtime", "list", "-j").Output()
	if err != nil {
		return nil
	}
	var byID map[string]Runtime
	if json.Unmarshal(out, &byID) != nil {
		return nil
	}
	list := make([]Runtime, 0, len(byID))
	for _, r := range byID {
		list = append(list, r)
	}
	return list
}

// Platform is the human name for a runtime's platform.
func (r Runtime) Platform() string {
	for _, p := range []struct{ key, name string }{
		{"iphone", "iOS"}, {"watch", "watchOS"}, {"appletv", "tvOS"}, {"xr", "visionOS"},
	} {
		if strings.Contains(r.PlatformIdentifier, p.key) {
			return p.name
		}
	}
	return "Simulator"
}

// MightBeRuntime is a cheap check for whether a path could be a runtime, so
// callers only shell out to simctl when it's worth it.
func MightBeRuntime(path string) bool {
	return strings.HasPrefix(path, "/System/Library/AssetsV2/") ||
		strings.HasPrefix(path, "/Library/Developer/CoreSimulator/")
}

// Delete removes the runtimes at the given paths. Paths that aren't a
// runtime are returned untouched for the caller to handle.
func Delete(paths []string) (deleted []string, failed map[string]error, rest []string) {
	failed = map[string]error{}
	var ids map[string]string
	for _, p := range paths {
		if !MightBeRuntime(p) {
			rest = append(rest, p)
			continue
		}
		if ids == nil {
			ids = map[string]string{}
			for _, r := range List(context.Background()) {
				ids[r.Path] = r.Identifier
			}
		}
		id, ok := ids[p]
		if !ok {
			rest = append(rest, p)
			continue
		}
		out, err := exec.Command("xcrun", "simctl", "runtime", "delete", id).CombinedOutput()
		if err != nil {
			failed[p] = fmt.Errorf("simctl runtime delete: %s", strings.TrimSpace(string(out)))
			continue
		}
		deleted = append(deleted, p)
	}
	return deleted, failed, rest
}
