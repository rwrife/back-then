# back-then 🕰️

**A local-first time machine for your files.**

You don't remember the filename. You remember *roughly when* it happened — "that PDF from around the Berlin trip," "the spreadsheet from tax season," "whatever I downloaded the day everything broke." `back-then` finds it by **time and circumstance**, not by name.

- 🔌 **100% offline.** No cloud, no account, no telemetry. Your files never leave the machine.
- 🧠 **Episodic search.** Reconstructs the *sessions* of files that arrived together, so you can browse time instead of folders.
- ⚡ **Fast & tiny.** A single Go binary backed by a local SQLite index. Runs on a potato.
- 🖥️ **Cross-platform.** Windows, macOS, Linux.

> ⚠️ Early days — see [PLAN.md](./PLAN.md) for the roadmap. M1 (scaffolding), M2 (the local index), and M3 (fuzzy-time `find`) are done: `index`, `stats`, and `find` work today. `sessions`/`near` are on the way.

## Install / build

Requires [Go](https://go.dev/dl/) 1.23+.

```sh
# Build the binary
go build -o back-then ./cmd/back-then

# ...or run straight from source
go run ./cmd/back-then version
```

That's it today — a single static binary, no runtime to install.

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
how close their timestamp sits to that window — closest first.

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

Use `--limit N` to change how many results are shown (default 20) and `--json`
for scripting — the JSON includes the resolved window so you can see how your
phrase was interpreted:

```sh
back-then find "last spring" --limit 50
back-then find "last spring" --json
```

## Quickstart (planned)

```sh
# Browse the episodes your files naturally cluster into
back-then sessions

# The killer move: what arrived around the same time as this file?
back-then near ~/Downloads/mystery.pdf
```

Add `--json` to any command for scripting.

## Why?

Native search (Spotlight, Windows Search, `find`) assumes you know a keyword or exact name. Cloud "memory" search works great — but only if you upload everything. `back-then` fills the gap: **offline, name-free, time-and-context-first** file recall. Inspired by the loud 2026 demand for local-first tools that just live on your hard drive.

## Status

Pre-alpha. Watch the [milestones](https://github.com/rwrife/back-then/issues) to follow along.

## License

MIT
