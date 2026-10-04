# springclean

your mac collects a frankly embarrassing amount of garbage. xcode derived data nobody asked for, sixteen copies of `node_modules` from projects you renamed in 2023, a git worktree you forgot existed, an app you opened once in 2024 and then never again. springclean walks your disk, finds the gunk, and lets you mark what goes in the trash.

everything goes through finder by default, so "put back" still works. if you'd rather skip the bin for something you were going to reinstall anyway, `X` deletes outright and asks twice first. you stay in charge — it just shows you what's there.

designed in a fit of disk-pressure panic, built in claude.

## install

```bash
go install github.com/0xdeafcafe/springclean@latest
```

or build it:

```bash
go build -o springclean .
```

## run

```bash
./springclean
```

opens a tui. shows everything it found grouped by category. mark items, hit apply, watch the bytes-reclaimed counter tick up. it's quite satisfying.

`←/→` move between the category tabs, and only the tabs that actually have something in them. `/` is a regex search over paths, reasons and project names, smartcase like vim: `node` matches anything, `Node` only the capitalised. it narrows whichever tab you're on, `esc` clears it. `o` opens the scan options.

if you'd rather work from a yaml file:

```bash
./springclean scan -o report.yaml
# edit report.yaml, set `marked: true` on what you want gone
./springclean apply report.yaml
```

## without the tui

for scripts and coding agents. scan once, then slice the report as often as you like:

```bash
./springclean scan --scope=full -o report.yaml
./springclean summary report.yaml --top 40
./springclean summary report.yaml --category git_worktree --older-than 14
./springclean summary report.yaml --category dev_cache --json
./springclean mark report.yaml <id-or-path>...
./springclean apply report.yaml -y
```

`summary` prints totals per category and the largest items with what deleting each one frees, its footprint including shared storage, and how many days it has sat idle. `scan --summary` prints the same thing straight after a scan.

a scan never waits on a macos privacy prompt: a folder that does not open within five seconds is skipped and listed under `unreadable` in the report.

## what it finds

- **dev cache** — `node_modules`, `target`, `.next`, `.venv`, `dist`, `Pods`, you know the drill
- **package cache** — npm, yarn, pnpm, pip, cargo, gradle, homebrew, go, bun, deno, maven, cocoapods
- **app cache & logs** — `~/Library/Caches`, `~/Library/Logs`, vs code, slack, chrome, etc.
- **xcode** — derived data, archives, ios/watchos/tvos device support, simulator caches
- **git worktrees** — linked worktrees from `git worktree add` that nobody has touched in 14+ days, with a warning when one still holds uncommitted or unpushed work
- **gitignored cruft** — big, stale, gitignored files and folders that no hardcoded name list would catch (opt in with `--ignored`)
- **old downloads** — anything in `~/Downloads` you haven't touched in 90+ days
- **large files** — anything >500MB lurking somewhere in `$HOME`
- **unused apps** — `.app` bundles in `/Applications` you haven't opened in 180+ days (asks spotlight via `kMDItemLastUsedDate`)
- **large folders** — any folder holding 1GB+ that nothing above explains: model stores, forgotten tmp dirs, old vms. sized bottom-up, so it names the tightest folder that holds the bulk, never `~` or `/Users`
- **simulator runtimes** — the ios/watchos/tvos runtimes xcode downloads onto their own disk images, invisible to a file walk. asked of `simctl`, and removed through it too
- **trash** — for when you forgot to empty it

## scopes

four flavours:

- `--scope=full` (default) — curated + walks every folder on the disk it can read. finds the big stuff no list would name. takes a few minutes on a full disk.
- `--scope=curated` — just the known caches, unused apps and simulator runtimes. seconds, no walk.
- `--scope=home` — curated + walks your entire `$HOME` looking for `node_modules`, worktrees, big files, etc.
- `--scope=root --root=/some/path` — walks anywhere you point it.

worktrees, large files, large folders and gitignored cruft only turn up in the walking scopes.

the walk stays out of trouble on its own: other volumes, `/System` and autofs mounts are skipped, app bundles and photo libraries are sized as one lump, and a folder with more than 2000 subfolders (git objects, package stores) is sized without being walked into. dev-cache names like `build` or `env` only count outside `Library` and system folders. curated never touches the filesystem beyond the known cache paths, so pointing it at a project and expecting worktrees won't work.

you don't have to remember any of that. press `[o]` on the splash screen for the scan options: scope, which folder, how stale a worktree has to be, whether to ask git about ignored files, whether to work out real disk use. `[o]` again from the results re-runs with different settings.

the scan runs at low priority, so it uses spare cores without getting in the way of whatever else you're doing. sizes of things it measures whole (`node_modules`, app bundles, worktrees, package stores) are remembered for a day and reused while the folder is unchanged, which makes a rescan much quicker. `--fresh` measures everything again.

from the shell there are shorter forms than spelling out the scope:

```bash
springclean --here              # scan the directory you're standing in
springclean worktrees           # same, and open on the worktree category
springclean worktrees ~/code    # or point it somewhere
```

## git worktrees

worktrees are found by their `.git` pointer file, so submodules and ordinary checkouts are left alone. a worktree is only flagged once it's sat untouched for `--worktree-age` days (14 by default; `-1` reports every one it finds).

"untouched" means the newest source file inside it, not the directory's own mtime. that barely moves when you edit files, and gets bumped by things that aren't you. dependency and build directories don't count either, so an `npm install` won't make abandoned work look active.

before anything gets trashed, springclean asks git whether the worktree still holds work that only exists there: uncommitted changes, unpushed commits, or a branch with no upstream at all. those show up with a `⚠` in the list and get spelled out again at the confirmation prompt. a worktree that's clean and fully pushed is safe to lose; one that isn't, isn't.

trashing a worktree directory doesn't deregister it, and git goes on listing it as prunable, so apply runs `git worktree prune` in the owning repo afterwards. `--no-prune` skips that.

## caches and the project around them

a `node_modules` tells you when it was last installed, which is not the same question as whether you still need it. one sitting under a branch you pushed this morning is holding up live work; the identical directory under a branch nobody has opened since spring is just bytes.

so caches are tagged with the checkout they were found in, and when that checkout was last worked on. the details panel shows both, and `--cache-age` filters on it:

```bash
springclean --here --cache-age 7   # only caches whose project has been idle a week
```

worktrees are dated from their newest source file, which is exact. every other checkout is dated from git's index, which moves on commit, checkout, add, rebase and even a bare `git status`. that costs one stat instead of walking the tree, and it errs towards looking recently used, so the failure mode is leaving a stale cache on disk rather than offering up one you're still using.

## gitignored cruft

`--ignored` asks git for everything it's been told to ignore in each repo the scan crosses, and flags whatever is both bigger than 50 MB and older than `--ignored-age` days (30 by default). this is the catch-all behind the curated name list: the data dumps, generated fixtures and stray archives that are specific to your project.

directories holding checkouts are left alone. keeping worktrees somewhere gitignored is normal, and collapsing that into a single suspect would offer all of them for deletion at once, so those are walked through and judged individually instead.

## how big things really are

sizes are what deleting something would actually give you back, which is not what adding up its files says.

three things get in the way. small files occupy a whole block each, so a directory of ten thousand tiny ones takes more room than its bytes suggest. hard links are one set of blocks under several names. and apfs `clonefile` gives two files their own inodes while they share every block, which is how pnpm installs a package into fifty checkouts, and how `du` comes to report fifty times the space that deleting all fifty would free.

so springclean counts blocks rather than bytes, and asks the filesystem where each file physically lives so a second copy of something is charged what it really costs. the details panel spells out the difference when there is one:

```
Frees        44 MB
Occupies     2.4 GB
Shared       2.4 GB with other copies
```

the whole set has to go before shared space comes back, so every copy is still listed — the fiftieth one frees nothing on its own, and hiding it would leave a pile of disk that can never be reclaimed and never appears. `⧉` in the list marks a row whose size is small for that reason.

shared blocks are charged to whichever copy the scan reached first, so a total over any set of items is exactly what deleting that set frees, while a single row is only exact for the copy that owns the blocks. that is why fifty near-identical worktrees each report a few hundred megabytes rather than one reporting everything: the arithmetic across them still adds up.

this costs one `open` per file. `--no-dedupe` skips it for a faster scan, at the price of counting every clone in full, and `[o]` has the same toggle.

## the move-to-trash bit

uses finder via osascript by default, which preserves "put back" metadata so you can drag stuff out of the bin if you regret it. if macos blocks the automation prompt, rerun with `--manual-trash` and it'll move files to `~/.Trash` directly (no put-back, but it works).

`X` deletes instead, skipping the trash entirely. this exists because the trash is a bad deal for a `node_modules`: finder moves a hundred thousand files in, then makes you watch it delete them one at a time when you empty the bin, all to protect something you were going to reinstall anyway. it asks twice, and nothing comes back. `springclean apply report.yaml --delete` does the same from the shell.

## things it deliberately doesn't touch

- `~/Library/Containers/com.docker.docker/Data/vms` — that's `Docker.raw`, a pre-allocated sparse image holding every docker image, container and volume you've ever had. trashing it would be a bad day. shrink it via docker desktop → resources → disk image size.
- anything inside `.git` directories
- mail, messages, photos library, icloud drive, and other things that are irreplaceable if you fat-finger them

## warning

this is a vibe coded disk cleaner. read the report before applying it. the trash is reversible but it's not a great party trick to delete your dissertation.
