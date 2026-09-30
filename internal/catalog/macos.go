package catalog

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/0xdeafcafe/springclean/internal/domain"
)

type KnownLocation struct {
	Path        string
	Category    domain.Category
	Reason      string
	Regenerable bool
	IsContents  bool
}

func KnownLocations() []KnownLocation {
	return []KnownLocation{
		{Path: "~/Library/Caches", Category: domain.CatAppCache, Reason: "Application caches", Regenerable: true, IsContents: true},
		{Path: "~/Library/Logs", Category: domain.CatAppLog, Reason: "Application logs", Regenerable: true, IsContents: true},

		{Path: "~/Library/Developer/Xcode/DerivedData", Category: domain.CatXcode, Reason: "Xcode build artifacts", Regenerable: true, IsContents: true},
		{Path: "~/Library/Developer/Xcode/Archives", Category: domain.CatXcode, Reason: "Xcode archives (irreplaceable if unsigned)", Regenerable: false},
		{Path: "~/Library/Developer/Xcode/iOS DeviceSupport", Category: domain.CatXcode, Reason: "iOS device support files (regenerated on connect)", Regenerable: true, IsContents: true},
		{Path: "~/Library/Developer/Xcode/watchOS DeviceSupport", Category: domain.CatXcode, Reason: "watchOS device support files", Regenerable: true, IsContents: true},
		{Path: "~/Library/Developer/Xcode/tvOS DeviceSupport", Category: domain.CatXcode, Reason: "tvOS device support files", Regenerable: true, IsContents: true},
		{Path: "~/Library/Developer/CoreSimulator/Caches", Category: domain.CatXcode, Reason: "Simulator caches", Regenerable: true, IsContents: true},
		{Path: "~/Library/Developer/CoreSimulator/Devices", Category: domain.CatXcode, Reason: "Simulator devices (state will be lost!)", Regenerable: false},

		{Path: "~/.npm/_cacache", Category: domain.CatPkgCache, Reason: "npm cache", Regenerable: true},
		{Path: "~/.npm/_logs", Category: domain.CatPkgCache, Reason: "npm logs", Regenerable: true},
		{Path: "~/.yarn/cache", Category: domain.CatPkgCache, Reason: "yarn cache", Regenerable: true},
		{Path: "~/.cache/yarn", Category: domain.CatPkgCache, Reason: "yarn (legacy) cache", Regenerable: true},
		{Path: "~/Library/pnpm/store", Category: domain.CatPkgCache, Reason: "pnpm content-addressable store", Regenerable: true},
		{Path: "~/Library/Caches/Homebrew", Category: domain.CatPkgCache, Reason: "Homebrew downloads cache", Regenerable: true},
		{Path: "~/Library/Caches/pip", Category: domain.CatPkgCache, Reason: "pip wheel cache", Regenerable: true},
		{Path: "~/.cache/pip", Category: domain.CatPkgCache, Reason: "pip cache (XDG)", Regenerable: true},
		{Path: "~/Library/Caches/pypoetry", Category: domain.CatPkgCache, Reason: "poetry cache", Regenerable: true},
		{Path: "~/.cargo/registry/cache", Category: domain.CatPkgCache, Reason: "Cargo registry cache", Regenerable: true},
		{Path: "~/.cargo/registry/src", Category: domain.CatPkgCache, Reason: "Cargo source cache", Regenerable: true},
		{Path: "~/.cargo/git", Category: domain.CatPkgCache, Reason: "Cargo git checkouts", Regenerable: true},
		{Path: "~/.gradle/caches", Category: domain.CatPkgCache, Reason: "Gradle caches", Regenerable: true},
		{Path: "~/.gradle/daemon", Category: domain.CatPkgCache, Reason: "Gradle daemon logs", Regenerable: true},
		{Path: "~/Library/Caches/CocoaPods", Category: domain.CatPkgCache, Reason: "CocoaPods cache", Regenerable: true},
		{Path: "~/.m2/repository", Category: domain.CatPkgCache, Reason: "Maven local repository", Regenerable: true},
		{Path: "~/Library/Caches/go-build", Category: domain.CatPkgCache, Reason: "Go build cache", Regenerable: true},
		{Path: "~/go/pkg/mod/cache", Category: domain.CatPkgCache, Reason: "Go module cache", Regenerable: true},
		{Path: "~/.bun/install/cache", Category: domain.CatPkgCache, Reason: "Bun cache", Regenerable: true},
		{Path: "~/.deno/cache", Category: domain.CatPkgCache, Reason: "Deno cache", Regenerable: true},

		// NOTE: ~/Library/Containers/com.docker.docker/Data/vms is intentionally
		// NOT listed. It contains Docker.raw — a pre-allocated sparse disk image
		// holding all images/containers/volumes. Trashing it would destroy every
		// Docker artifact, and the size shown there is reserved space, not
		// reclaimable. The user shrinks it via Docker Desktop → Resources →
		// Disk image size, or by pruning inside Docker.
		{Path: "~/Library/Group Containers/group.com.docker", Category: domain.CatDocker, Reason: "Docker Desktop group container", Regenerable: false},

		{Path: "~/.Trash", Category: domain.CatTrash, Reason: "Trash (will permanently delete!)", Regenerable: false, IsContents: true},

		{Path: "~/Library/Application Support/Code/CachedData", Category: domain.CatAppCache, Reason: "VS Code cached data", Regenerable: true, IsContents: true},
		{Path: "~/Library/Application Support/Code/Cache", Category: domain.CatAppCache, Reason: "VS Code cache", Regenerable: true, IsContents: true},
		{Path: "~/Library/Application Support/Code/CachedExtensions", Category: domain.CatAppCache, Reason: "VS Code cached extensions", Regenerable: true, IsContents: true},
		{Path: "~/Library/Application Support/Slack/Cache", Category: domain.CatAppCache, Reason: "Slack cache", Regenerable: true, IsContents: true},
		{Path: "~/Library/Application Support/Slack/Service Worker/CacheStorage", Category: domain.CatAppCache, Reason: "Slack service worker cache", Regenerable: true, IsContents: true},
		{Path: "~/Library/Application Support/Google/Chrome/Default/Cache", Category: domain.CatAppCache, Reason: "Chrome cache", Regenerable: true, IsContents: true},
		{Path: "~/Library/Application Support/Firefox/Profiles", Category: domain.CatAppCache, Reason: "Firefox profile data (cache only inside)", Regenerable: true, IsContents: true},
		{Path: "~/Library/Caches/com.apple.Safari/WebKitCache", Category: domain.CatAppCache, Reason: "Safari WebKit cache", Regenerable: true, IsContents: true},
		{Path: "~/Library/Caches/com.spotify.client", Category: domain.CatAppCache, Reason: "Spotify cache", Regenerable: true, IsContents: true},

		{Path: "~/Library/Mobile Documents/com~apple~CloudDocs/.Trash", Category: domain.CatTrash, Reason: "iCloud Drive Trash", Regenerable: false, IsContents: true},
	}
}

// StopMarkers are directory basenames that, when found during a $HOME walk,
// should be emitted as a single suspect rather than recursed into.
func StopMarkers() map[string]StopMarker {
	return map[string]StopMarker{
		"node_modules":     {Category: domain.CatDevCache, Reason: "Node.js dependencies (re-installable via package manager)", Regenerable: true},
		".venv":            {Category: domain.CatDevCache, Reason: "Python virtualenv", Regenerable: true},
		"venv":             {Category: domain.CatDevCache, Reason: "Python virtualenv", Regenerable: true},
		"env":              {Category: domain.CatDevCache, Reason: "Python virtualenv (likely)", Regenerable: true},
		"__pycache__":      {Category: domain.CatDevCache, Reason: "Python bytecode cache", Regenerable: true},
		".pytest_cache":    {Category: domain.CatDevCache, Reason: "pytest cache", Regenerable: true},
		".mypy_cache":      {Category: domain.CatDevCache, Reason: "mypy cache", Regenerable: true},
		".ruff_cache":      {Category: domain.CatDevCache, Reason: "ruff cache", Regenerable: true},
		".tox":             {Category: domain.CatDevCache, Reason: "tox cache", Regenerable: true},
		"target":           {Category: domain.CatDevCache, Reason: "Rust target directory", Regenerable: true},
		".next":            {Category: domain.CatDevCache, Reason: "Next.js build cache", Regenerable: true},
		".nuxt":            {Category: domain.CatDevCache, Reason: "Nuxt build cache", Regenerable: true},
		".turbo":           {Category: domain.CatDevCache, Reason: "Turborepo cache", Regenerable: true},
		".parcel-cache":    {Category: domain.CatDevCache, Reason: "Parcel cache", Regenerable: true},
		".svelte-kit":      {Category: domain.CatDevCache, Reason: "SvelteKit build", Regenerable: true},
		"dist":             {Category: domain.CatDevCache, Reason: "Build output (regenerable)", Regenerable: true},
		"build":            {Category: domain.CatDevCache, Reason: "Build output (regenerable)", Regenerable: true},
		".gradle":          {Category: domain.CatDevCache, Reason: "Gradle project cache", Regenerable: true},
		".idea":            {Category: domain.CatDevCache, Reason: "JetBrains project metadata", Regenerable: true},
		"DerivedData":      {Category: domain.CatXcode, Reason: "Xcode derived data (project-local)", Regenerable: true},
		"Pods":             {Category: domain.CatDevCache, Reason: "CocoaPods (re-installable via pod install)", Regenerable: true},
		"vendor":           {Category: domain.CatDevCache, Reason: "Vendored dependencies (re-installable)", Regenerable: true},
		"bower_components": {Category: domain.CatDevCache, Reason: "Bower dependencies", Regenerable: true},
		"elm-stuff":        {Category: domain.CatDevCache, Reason: "Elm compiler cache", Regenerable: true},
		".dart_tool":       {Category: domain.CatDevCache, Reason: "Dart/Flutter tool cache", Regenerable: true},
	}
}

type StopMarker struct {
	Category    domain.Category
	Reason      string
	Regenerable bool
}

// SkipDuringHomeWalk lists path prefixes we never descend into during a $HOME walk
// because they're handled by the curated catalog or contain irreplaceable user data.
func SkipDuringHomeWalk() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, "Library", "Caches"),
		filepath.Join(home, "Library", "Logs"),
		filepath.Join(home, "Library", "Developer"),
		filepath.Join(home, "Library", "Containers"),
		filepath.Join(home, "Library", "Group Containers"),
		filepath.Join(home, "Library", "Application Support"),
		filepath.Join(home, "Library", "Mail"),
		filepath.Join(home, "Library", "Messages"),
		filepath.Join(home, "Library", "Safari"),
		filepath.Join(home, "Library", "Calendars"),
		filepath.Join(home, "Library", "Photos"),
		filepath.Join(home, "Library", "Mobile Documents"),
		filepath.Join(home, "Pictures", "Photos Library.photoslibrary"),
		filepath.Join(home, "Pictures", "Photo Library.photolibrary"),
		filepath.Join(home, ".Trash"),
		filepath.Join(home, ".git"),
	}
}

// FDAProbePaths are paths whose readability indicates Full Disk Access is granted.
func FDAProbePaths() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, "Library", "Mail"),
		filepath.Join(home, "Library", "Safari"),
		filepath.Join(home, "Library", "Messages"),
		filepath.Join(home, "Library", "Calendars"),
		filepath.Join(home, "Library", "Application Support", "AddressBook"),
	}
}

// ExpandHome rewrites a leading ~/ to the user's home directory.
func ExpandHome(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

// DownloadAge is the cutoff for flagging stale files under ~/Downloads.
const DownloadAgeDays = 90

// LargeFileThreshold is the minimum file size to flag as "large_file" during a $HOME walk.
const LargeFileThreshold int64 = 500 * 1024 * 1024 // 500 MB

// UnusedAppThresholdDays is how long an app must be unopened to flag.
const UnusedAppThresholdDays = 180

// WorktreeAgeDays is how long a linked git worktree must sit untouched before
// it's flagged. Age is measured from the newest source file inside it, not the
// directory's own mtime. See scan.contentModTime.
const WorktreeAgeDays = 14

// IgnoredCruftAgeDays is how long a gitignored file or directory must sit
// untouched before it's flagged.
const IgnoredCruftAgeDays = 30

// IgnoredCruftMinSize is the floor for flagging gitignored cruft. Well below
// LargeFileThreshold, because the point is catching accumulated build output
// and dumps that individually look small.
const IgnoredCruftMinSize int64 = 50 * 1024 * 1024 // 50 MB

// regenerableNames is derived once at init. Regenerable is called for every
// directory entry during a walk, so it can't afford to rebuild the marker map.
var regenerableNames = func() map[string]bool {
	out := map[string]bool{}
	for name, m := range StopMarkers() {
		if m.Regenerable {
			out[name] = true
		}
	}
	return out
}()

// Regenerable reports whether a directory name is a known build/dependency
// directory: content that a package manager or build step can recreate.
// Such directories are excluded when dating a worktree, since reinstalling
// dependencies shouldn't make abandoned work look freshly touched.
func Regenerable(name string) bool {
	return regenerableNames[name]
}

// LargeDirThreshold is how much of a folder has to be unexplained by any
// other finding before the folder itself is reported.
const LargeDirThreshold int64 = 1 << 30 // 1 GiB

// FanoutLimit is how many subdirectories a directory may hold before the walk
// stops descending into it and sizes it as one lump instead. Past this it is a
// content store (git objects, a pnpm store, a mail cache) where no individual
// subdirectory is worth reporting and walking each one is all cost.
const FanoutLimit = 2000

// SkipDuringFullWalk lists absolute paths a whole-disk walk never enters:
// the sealed system volume, other mounts, autofs points that can hang, and
// kernel-managed files nothing should touch. Other devices are also skipped
// by the walk itself, so this only needs the ones reachable on the data volume.
func SkipDuringFullWalk() []string {
	paths := []string{
		"/System", "/Volumes", "/dev", "/net", "/home", "/Network",
		"/private/var/vm", "/private/var/db", "/private/var/folders",
		"/.Spotlight-V100", "/.fseventsd", "/.DocumentRevisions-V100", "/.MobileBackups",
		"/Library/Developer/CoreSimulator/Volumes",
		"/Library/Developer/CoreSimulator/Cryptex",
	}
	// Already reported entry by entry by the curated catalog.
	for _, loc := range KnownLocations() {
		paths = append(paths, ExpandHome(loc.Path))
	}
	return paths
}

// bundleExts are directory extensions macOS presents as a single file. They
// are sized whole and never walked into.
var bundleExts = map[string]bool{
	".app": true, ".photoslibrary": true, ".photolibrary": true, ".musiclibrary": true,
	".tvlibrary": true, ".sparsebundle": true, ".xcarchive": true, ".framework": true,
	".bundle": true, ".utm": true, ".pvm": true, ".vmwarevm": true, ".lrdata": true,
	".fcpbundle": true, ".logicx": true, ".imovielibrary": true,
}

// IsBundle reports whether a directory name is a package macOS treats as one file.
func IsBundle(name string) bool {
	return bundleExts[strings.ToLower(filepath.Ext(name))]
}

// MarkersApply reports whether dev-cache markers (node_modules, build, env…)
// mean anything under this path. Inside Library folders and system trees a
// directory named `build` or `env` is an app's own data, not project output.
func MarkersApply(path string) bool {
	if strings.Contains(path+"/", "/Library/") {
		return false
	}
	for _, p := range []string{"/Applications/", "/private/", "/usr/", "/opt/", "/System/", "/cores/"} {
		if strings.HasPrefix(path+"/", p) {
			return false
		}
	}
	return true
}

// ProtectedDir reports whether a folder is too broad to ever offer as a large
// folder: deleting it would take out a user, the OS, or a whole class of data.
// Its size still counts towards the folders above it.
func ProtectedDir(path string) bool {
	home, _ := os.UserHomeDir()
	switch path {
	case "/", "/Users", "/Applications", "/Library", "/opt", "/private", "/private/var",
		"/usr", "/usr/local", "/opt/homebrew", home:
		return true
	}
	parent, base := filepath.Dir(path), filepath.Base(path)
	if base == ".git" || base == ".hg" || base == ".svn" {
		// A repository's history. Big, but not something to throw away whole.
		return true
	}
	if parent == "/Users" {
		return true
	}
	if filepath.Dir(parent) == "/Users" {
		switch base {
		case "Library", "Documents", "Desktop", "Downloads", "Movies", "Music", "Pictures", "Applications":
			return true
		}
	}
	if filepath.Base(parent) == "Library" {
		switch base {
		case "Application Support", "Containers", "Group Containers", "Developer", "Mobile Documents":
			return true
		}
	}
	return false
}
