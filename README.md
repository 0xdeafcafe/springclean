# springclean

your mac collects a frankly embarrassing amount of garbage. xcode derived data nobody asked for, sixteen copies of `node_modules` from projects you renamed in 2023, a git worktree you forgot existed, an app you opened once in 2024 and then never again. springclean walks your disk, finds the gunk, and lets you mark what goes in the trash.

nothing gets `rm -rf`'d. everything goes through finder so "put back" still works. you stay in charge — it just shows you what's there.

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

if you'd rather work from a yaml file:

```bash
./springclean scan -o report.yaml
# edit report.yaml, set `marked: true` on what you want gone
./springclean apply report.yaml
```

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
- **trash** — for when you forgot to empty it

## scopes

three flavours:

- `--scope=curated` (default) — just the known caches and unused apps. fast, safe, no surprises.
- `--scope=home` — curated + walks your entire `$HOME` looking for `node_modules`, worktrees, big files, etc.
- `--scope=root --root=/some/path` — walks anywhere you point it.

worktrees, large files and gitignored cruft only turn up in the two walking scopes. curated never touches the filesystem beyond the known cache paths, so pointing it at a project and expecting worktrees won't work.

you don't have to remember any of that. press `[o]` on the splash screen for the scan options: scope, which folder, how stale a worktree has to be, whether to ask git about ignored files. `[o]` again from the results re-runs with different settings.

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

## gitignored cruft

`--ignored` asks git for everything it's been told to ignore in each repo the scan crosses, and flags whatever is both bigger than 50 MB and older than `--ignored-age` days (30 by default). this is the catch-all behind the curated name list: the data dumps, generated fixtures and stray archives that are specific to your project.

directories holding checkouts are left alone. keeping worktrees somewhere gitignored is normal, and collapsing that into a single suspect would offer all of them for deletion at once, so those are walked through and judged individually instead.

## the move-to-trash bit

uses finder via osascript by default, which preserves "put back" metadata so you can drag stuff out of the bin if you regret it. if macos blocks the automation prompt, rerun with `--manual-trash` and it'll move files to `~/.Trash` directly (no put-back, but it works).

## things it deliberately doesn't touch

- `~/Library/Containers/com.docker.docker/Data/vms` — that's `Docker.raw`, a pre-allocated sparse image holding every docker image, container and volume you've ever had. trashing it would be a bad day. shrink it via docker desktop → resources → disk image size.
- anything inside `.git` directories
- mail, messages, photos library, icloud drive, and other things that are irreplaceable if you fat-finger them

## warning

this is a vibe coded disk cleaner. read the report before applying it. the trash is reversible but it's not a great party trick to delete your dissertation.
