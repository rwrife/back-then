# back-then 🕰️

**A local-first time machine for your files.**

You don't remember the filename. You remember *roughly when* it happened — "that PDF from around the Berlin trip," "the spreadsheet from tax season," "whatever I downloaded the day everything broke." `back-then` finds it by **time and circumstance**, not by name.

- 🔌 **100% offline.** No cloud, no account, no telemetry. Your files never leave the machine.
- 🧠 **Episodic search.** Reconstructs the *sessions* of files that arrived together, so you can browse time instead of folders.
- ⚡ **Fast & tiny.** A single Go binary backed by a local SQLite index. Runs on a potato.
- 🖥️ **Cross-platform.** Windows, macOS, Linux.

> ⚠️ Early days — see [PLAN.md](./PLAN.md) for the roadmap. M1–M5 are done: `index`, `stats`, `find`, `sessions`, `near`, and `forget` work today, with EXIF capture dates, blended ranking, and `.backthenignore` scoping. M6 (polish + release) is in progress — cross-compiled release binaries now ship automatically on every version tag.

## Install

### Download a release binary (recommended)

No Go toolchain required. Grab the archive for your OS/arch from the
[latest release](https://github.com/rwrife/back-then/releases/latest),
extract it, and put the `back-then` binary on your `PATH`.

```sh
# Linux / macOS example (adjust VERSION + platform to match the asset name)
VERSION=v0.1.0
OS=linux            # or: darwin
ARCH=amd64          # or: arm64
curl -LO "https://github.com/rwrife/back-then/releases/download/${VERSION}/back-then_${VERSION}_${OS}_${ARCH}.tar.gz"
tar -xzf "back-then_${VERSION}_${OS}_${ARCH}.tar.gz"
sudo mv "back-then_${VERSION}_${OS}_${ARCH}/back-then" /usr/local/bin/

back-then version
```

On Windows, download the `..._windows_amd64.zip`, unzip it, and run
`back-then.exe`. Every release ships a `.sha256` next to each archive so you can
verify the download.

> Binaries are cross-compiled and published automatically by the
> [release workflow](.github/workflows/release.yml) on every version tag, so a
> stranger can go from zero to a working `back-then` in under a minute — no
> runtime to install.

### Build from source

Requires [Go](https://go.dev/dl/) 1.23+.

```sh
# Build the binary
go build -o back-then ./cmd/back-then

# ...or run straight from source
go run ./cmd/back-then version
```

That's it — a single static binary, no runtime to install.

```sh
$ back-then version
back-then dev (commit none, built unknown, go1.23 linux/amd64)

$ back-then version --short
dev

$ back-then --help      # lists every available command
```

Version metadata is stamped at release time via `-ldflags`.

## Indexing your files

Point `back-then` at one or more directories. It walks each tree and records
per-file signals — size, modified time, creation time (when the OS exposes
it), extension, and parent folder — into a local SQLite index. Nothing leaves
your machine, and **only metadata is read, never file contents**.

```sh
# Build (or update) the index for these trees
back-then index ~/Downloads ~/Documents
```

```text
Indexed /home/you/.config/back-then/index.db: 8423 files seen, 8423 updated, 0 unchanged.
```

Indexing is **incremental**: files whose size and modified time haven't
changed since the last run are skipped, so re-indexing the same tree is fast.

```text
$ back-then index ~/Downloads ~/Documents
Indexed /home/you/.config/back-then/index.db: 8423 files seen, 12 updated, 8411 unchanged.
```

Noisy machine-generated trees (`.git`, `node_modules`, caches, build output,
and similar) are skipped by default. Add more with `--skip`:

```sh
back-then index ~/code --skip target --skip out
```

### Scoping what gets indexed with `.backthenignore`

For finer control, drop a `.backthenignore` file in any directory you index.
It uses a familiar **gitignore-style** syntax and applies to that directory
and everything below it:

```gitignore
# back-then ignore file
*.log            # skip log files anywhere below here
secret.txt       # skip a specific file by name
cache/           # trailing slash: skip directories named "cache"
/build           # leading slash: only the build/ next to this file
logs/*.tmp       # a path pattern, relative to this file
!keep.log        # "!" re-includes something an earlier rule skipped
```

Rules:

- One pattern per line; blank lines and `#` comments are ignored.
- A pattern with **no slash** matches by base name at any depth (`*.log`).
- A **trailing `/`** matches directories only (and prunes everything inside).
- A **leading `/`** anchors the pattern to the directory holding the ignore
  file; a pattern that otherwise contains a `/` is matched against the path
  relative to that directory.
- A leading **`!`** negates (re-includes) a path a previous rule ignored.
- Nested `.backthenignore` files stack: a deeper file can override a
  shallower one, and within a file the **last matching rule wins**.

The default skip list still applies on top of your ignore files. To index
everything the skip list allows and ignore any `.backthenignore` files, pass
`--no-ignore-file`:

```sh
back-then index ~/code --no-ignore-file
```

The default-skipped directory names are: `.git`, `.hg`, `.svn`,
`node_modules`, `bower_components`, `vendor`, `.cache`, `.npm`, `.pnpm-store`,
`__pycache__`, `.venv`, `venv`, `.idea`, `.vscode`, `.terraform`, `.gradle`,
`.next`, `.nuxt`, `dist`, `build`, `target`, `.mypy_cache`, `.pytest_cache`,
`.ruff_cache`, `.tox`, `.bundle`, `.DS_Store`, `.Trash`, `.trash`.

The index lives in your user config directory by default; override it with
`--db /path/to/index.db` (handy for per-volume or throwaway indexes).

## Inspecting the index

`back-then stats` summarizes what's been indexed — file count, total size, the
span of modified times, and the most common extensions:

```text
$ back-then stats
Files:      8423
Total size: 14.2 GiB
Date span:  2017-08-02 → 2026-06-29
Top extensions:
  .jpg   2310
  .pdf   1182
  .png    944
  .txt    611
  .zip    402
```

Add `--json` for scripting (and `--top N` to change how many extensions are
listed):

```sh
back-then stats --json
```

## Finding files by *roughly when*

This is the point of `back-then`. Instead of a filename, give it a fuzzy time
phrase. It resolves the phrase into a date window, then ranks indexed files by
how well they match — closest in time first, with two lighter signals breaking
ties between files at a similar distance.

```text
$ back-then find "last spring"
Window: 2025-03-01 → 2025-06-01  (7 matches)
SCORE  WHEN              SIZE      PATH
1.00   2025-04-18 09:12  2.1 MiB   /home/you/Downloads/berlin-itinerary.pdf
1.00   2025-05-02 14:55  88.0 KiB  /home/you/Documents/packing-list.md
0.71   2025-06-09 17:30  3.4 MiB   /home/you/Downloads/hotel-receipt.pdf
```

Lots of phrasings work — relative periods, counts, months, seasons, years, and
explicit dates:

```sh
back-then find "today"
back-then find "this month"
back-then find "3 weeks ago"
back-then find "around march"
back-then find "december 2024"
back-then find "last winter"
back-then find "2018"
back-then find "2024-03-15"
```

Filler words (`around`, `the`, `sometime`, `in`) are ignored, and matching is
case-insensitive. Files inside the window score `1.00`; files just outside
decay smoothly with distance, so near-misses still surface.

### How ranking blends signals

Time proximity is the primary driver and sets a ceiling on every file's score.
Two secondary signals then lift files toward that ceiling to break ties between
candidates at a similar distance — they can reorder near-neighbors but never let
a far-off file leapfrog a closer one:

- **Signal richness** — files carrying trustworthy metadata rank higher. An
  EXIF capture time (a real "when this happened" signal) counts most, with a
  known extension and a plausible non-empty size adding a little more. A
  well-documented photo edges out a zero-byte, extensionless scratch file at
  the same distance.
- **Folder cohesion** — files that belong to a burst reinforce each other. A
  folder packed with near-window files (a trip, a shoot, an import) scores as a
  tight cluster, so a photo from that event outranks an otherwise-identical
  file sitting alone in its own folder.

Because the secondary signals are proximity-weighted, a folder that merely
contains many far-off files doesn't masquerade as a cluster, and every score
stays within `[0, 1]`.

Use `--limit N` to change how many results are shown (default 20) and `--json`
for scripting — the JSON includes the resolved window so you can see how your
phrase was interpreted:

```sh
back-then find "last spring" --limit 50
back-then find "last spring" --json
```

## Browsing sessions

The payoff: instead of digging through folders, browse the **sessions** your
files naturally fall into — bursts that arrived or changed close together in
time. `back-then sessions` lists them newest-first, each with when it
happened, how many files, the dominant file types, and the folder most of
those files lived in:

```text
$ back-then sessions
WHEN                    FILES  TYPES          TOP FOLDER
2026-06-29 14:02–14:19  37     .jpg×31 .mov×4  …/Photos/2026-06-29
2026-06-12 09:41–10:03  6      .pdf×4 .docx×2  …/Documents/taxes
2026-05-30 21:10        1      .zip×1          …/Downloads
```

Sessions are split wherever the gap between consecutive files gets large. The
split is **folder-aware** — files staying in the same directory tolerate a
wider lull before a new session begins. Tune the base gap with `--gap`:

```sh
back-then sessions --gap 90m     # tighter bursts
back-then sessions --gap 6h      # looser grouping
back-then sessions --json        # machine-readable
```

## `near` — what arrived together?

The killer move. Give `back-then` a file you *do* remember, and it surfaces
the other files from the same episode — everything that arrived around the
same time — ordered by how close in time they are:

```text
$ back-then near ~/Downloads/mystery.pdf
Around /home/you/Downloads/mystery.pdf (2026-06-12), within 6h0m0s:
OFFSET  WHEN              SIZE      PATH
+2m     2026-06-12 09:43  1.2 MiB   /home/you/Downloads/boarding-pass.pdf
+14m    2026-06-12 09:55  318 KiB   /home/you/Documents/taxes/w2.pdf
-31m    2026-06-12 09:10  4.0 MiB   /home/you/Downloads/hotel.png
```

The file must already be in the index. Widen or narrow the search window with
`--window`:

```sh
back-then near ~/Downloads/mystery.pdf --window 30m   # only the tight burst
back-then near ~/Downloads/mystery.pdf --window 24h   # the whole day
back-then near ~/Downloads/mystery.pdf --json
```

## `forget` — prune the index for privacy or space

Changed your mind about indexing a stretch of time? `back-then forget` removes
matching entries from the **index only** — it never touches the files on disk.
The time window is resolved exactly like `find`, so you can forget a fuzzy span
as easily as you searched it.

Because it's destructive, `forget` **previews by default** (a dry run) and only
deletes when you pass `--yes`:

```text
$ back-then forget "2019"
Window: 2019-01-01 → 2020-01-01  (128 entries in range)
Dry run: 128 entries would be pruned from the index (files on disk untouched). Re-run with --yes to apply.

$ back-then forget "2019" --yes
Window: 2019-01-01 → 2020-01-01  (128 entries in range)
Forgot 128 index entries. Files on disk are untouched.
```

```sh
back-then forget "last spring"        # preview what would be pruned
back-then forget "2019" --yes         # actually prune those entries
back-then forget "2019" --json        # machine-readable (reports matched/applied/removed)
```

Re-running `index` on the same paths re-adds the files, so `forget` is about the
current index, not permanence. For a hard privacy wipe, forget the range and
stop indexing that path.

## Quickstart (planned)

```sh
# Browse the episodes your files naturally cluster into
back-then sessions

# The killer move: what arrived around the same time as this file?
back-then near ~/Downloads/mystery.pdf

# Find by fuzzy time
back-then find "around last spring"
back-then find "the week of jun 3"
```

Add `--json` to any command for scripting.

## Why?

Native search (Spotlight, Windows Search, `find`) assumes you know a keyword or exact name. Cloud "memory" search works great — but only if you upload everything. `back-then` fills the gap: **offline, name-free, time-and-context-first** file recall. Inspired by the loud 2026 demand for local-first tools that just live on your hard drive.

## Status

Pre-alpha. Watch the [milestones](https://github.com/rwrife/back-then/issues) to follow along.

## License

MIT
