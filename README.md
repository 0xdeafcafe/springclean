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
- **git worktrees** — linked worktrees from `git worktree add`, with how long it's been since you last touched them
- **old downloads** — anything in `~/Downloads` you haven't touched in 90+ days
- **large files** — anything >500MB lurking somewhere in `$HOME`
- **unused apps** — `.app` bundles in `/Applications` you haven't opened in 180+ days (asks spotlight via `kMDItemLastUsedDate`)
- **trash** — for when you forgot to empty it

## scopes

three flavours:

- `--scope=curated` (default) — just the known caches and unused apps. fast, safe, no surprises.
- `--scope=home` — curated + walks your entire `$HOME` looking for `node_modules`, worktrees, big files, etc.
- `--scope=root --root=/some/path` — walks anywhere you point it.

## the move-to-trash bit

uses finder via osascript by default, which preserves "put back" metadata so you can drag stuff out of the bin if you regret it. if macos blocks the automation prompt, rerun with `--manual-trash` and it'll move files to `~/.Trash` directly (no put-back, but it works).

## things it deliberately doesn't touch

- `~/Library/Containers/com.docker.docker/Data/vms` — that's `Docker.raw`, a pre-allocated sparse image holding every docker image, container and volume you've ever had. trashing it would be a bad day. shrink it via docker desktop → resources → disk image size.
- anything inside `.git` directories
- mail, messages, photos library, icloud drive, and other things that are irreplaceable if you fat-finger them

## warning

this is a vibe coded disk cleaner. read the report before applying it. the trash is reversible but it's not a great party trick to delete your dissertation.
