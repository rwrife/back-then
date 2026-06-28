# back-then

> _"It was a PDF... I downloaded it around when I was planning the Berlin trip... last spring sometime?"_
> Cool. `back-then` finds it.

## 1. Pitch

`back-then` is a **local-first time machine for your filesystem**. Instead of searching by a filename you don't remember, you search by **roughly when** a file showed up and **what was going on around it** — "that spreadsheet from around tax season," "the photo from the week of the move," "stuff I touched the day everything broke." It reads only on-disk signals (modified/created times, EXIF capture dates, the burst of files that arrived together, the folder it lived in) and ranks candidates. No cloud, no account, no LLM required. Your files never leave the machine.

## 2. Trend inspiration

What I saw while scanning current trends (June 2026):

- **The anti-cloud movement is the single biggest unmet-app-gap signal.** An analysis of 9,300+ "I wish there was an app for this" Reddit posts found ~640 explicitly demanding *local-first / offline / self-hosted* tools — about 7% of all requests, the largest single cluster. The recurring quote: _"I want a photo organizer that lives on my hard drive... I just want to find my pictures from 2018 without an internet connection."_ — https://digitalbiztalk.com/article/what-9300-reddit-posts-reveal-about-app-gaps-in-2026
- **Windows 11's 2026 utility crop rewards focused, single-annoyance tools** over bloated suites, with a clear shift toward "cross-platform + local-first privacy." — https://windowsforum.com/threads/top-windows-11-utilities-of-2026-launchers-privacy-tools-and-workflow-boosters.414265/
- **The terminal renaissance** keeps minting fast, single-purpose TUIs (Yazi, Harlequin, Posting) that people actually adopt. A snappy CLI/TUI with one sharp job fits the moment. — https://1337skills.com/blog/2026-03-09-terminal-renaissance-modern-tui-tools-reshaping-developer-workflows/
- **Native OS search keeps failing the "I forgot the name" case.** Spotlight/Windows Search/`find` all assume you know a keyword or exact name. Nobody indexes *episodic* context (what arrived together, when, in what folder).

The throughline: people don't remember filenames. They remember **time and circumstance**. No mainstream tool searches that way, locally.

## 3. Why it's different

| Existing thing | What it does | Why `back-then` is different |
|---|---|---|
| Spotlight / Windows Search / `mdfind` | Full-text + name + a few attributes | Keyword/name-first. `back-then` is **time-and-context-first**: "around when," "what came with it," "what folder." |
| `find` / `fd` | Name/glob/time-range filters | Powerful but you must *specify exact predicates*. `back-then` takes fuzzy human time ("last spring") and **ranks by episodic proximity**, not just boolean match. |
| Recoll / DocFetcher | Local full-text desktop search | Indexes *contents*; needs you to recall words inside the file. `back-then` works even for binaries/photos/zips where you remember *nothing about the contents*. |
| Photo organizers (digiKam, etc.) | Photos only, cloud-optional | Photos only. `back-then` is **any file type** and is built around the "session/burst" idea, not albums. |
| Cloud "memory" search (Google Photos, etc.) | Great recall, **requires cloud + upload** | The exact thing the anti-cloud crowd is fleeing. `back-then` is **100% offline**. |

The fresh primitive: **the "session"** — a cluster of files that arrived/changed close together in time (and often in the same folder). Humans remember in episodes; `back-then` reconstructs those episodes from filesystem timestamps and lets you browse *time*, not folders.

## 4. MVP scope (v0.1)

The smallest genuinely useful thing:

- `back-then index <path>` — walk a directory tree, collect per-file signals (size, mtime, ctime, birthtime when available, extension, parent folder, EXIF DateTimeOriginal for images) into a local **SQLite** DB. Incremental (skip unchanged).
- `back-then find "<query>"` — parse a fuzzy time phrase ("last spring", "around march", "the week of jun 3", "2 years ago") into a date window, return the top N files ranked by time-proximity + signal richness.
- `back-then sessions` — list reconstructed sessions (time-clustered bursts of files) with a one-line summary each (when, count, dominant file types, top folder).
- `back-then near <file>` — "show me what arrived around the same time as this file" (the killer move).
- Output: clean table to stdout, `--json` flag for scripting. Respects `.gitignore`-style ignores + a sane default skip list (node_modules, .git, caches).
- 100% offline. One static binary-ish install. No network calls, ever.

That's a useful tool on day one.

## 5. Tech stack

Boring, fast, cross-platform:

- **Language: Go.** Single static binary, trivial cross-compile (Win/macOS/Linux), great `filepath.WalkDir`, fast cold start — perfect for a CLI people run ad hoc. No runtime to install (big deal for the "just give me an app" crowd).
- **Storage: SQLite** (via `modernc.org/sqlite`, pure-Go, no cgo) — zero external deps, the index is a single portable file, and time-window queries are trivial SQL.
- **Time parsing:** start with a small hand-rolled fuzzy-time parser (covers "last spring", "around <month>", "N days/weeks/months/years ago", explicit dates); keep it isolated so it's easy to grow.
- **EXIF:** a tiny dependency-light EXIF reader for image capture dates; degrade gracefully to mtime when absent.
- **CLI framework:** `cobra` (subcommands, help, flags — the boring standard).
- **Testing:** Go's stdlib `testing` + golden-file tests on a synthetic fixture tree.

Why not Python/Electron? No runtime baggage, no 200MB app, instant startup. Why not Rust? Go ships a working CLI in a day and that's the whole point.

## 6. Architecture

```
back-then/
  cmd/back-then/        # main(), cobra wiring
  internal/walk/        # filesystem scan + ignore rules + signal extraction
  internal/store/       # SQLite open/migrate, upsert files, query windows
  internal/exif/        # best-effort image capture-date reader
  internal/when/        # fuzzy time-phrase -> [start,end] window parser
  internal/sessions/    # time-clustering of files into "sessions"
  internal/rank/        # scoring: time proximity + signal richness + folder cohesion
  internal/render/      # table + --json output
```

Key flow: `index` → walk emits `FileSignal` records → `store` upserts. `find` → `when` parses query → `store` pulls candidate window → `rank` scores → `render` prints. `sessions`/`near` reuse the same store via clustering in `internal/sessions`.

## 7. Milestones (each shippable)

1. **M1 — scaffold + hello-world.** Go module, cobra CLI, `back-then version` + `back-then --help`. CI (GitHub Actions) builds + vets on Win/macOS/Linux. README quickstart. (Hello-world milestone.)
2. **M2 — indexer + SQLite store.** `back-then index <path>` walks a tree, extracts core signals (size/mtime/ctime/birthtime/ext/parent), upserts into SQLite with incremental skip. `back-then stats` prints index summary.
3. **M3 — fuzzy time `find`.** `internal/when` parser + `back-then find "<query>"` returns top-N ranked by time proximity. Table + `--json`. Golden tests for the time parser.
4. **M4 — sessions + `near`.** Time-clustering into sessions; `back-then sessions` lists them; `back-then near <file>` shows co-arriving files. The episodic-memory payoff.
5. **M5 — EXIF + smarter ranking.** Image capture-date via EXIF (fallback to mtime); ranking blends time proximity + signal richness + folder cohesion; ignore-file support (`.backthenignore` + default skip list).
6. **M6 — polish + release.** `back-then watch` (optional incremental re-index), config file, GitHub release with cross-compiled binaries, install instructions, demo asciicast in README.

## 8. Backlog / future features (v0.2+)

1. **`back-then timeline`** — a TUI scrubber: drag across a calendar/time axis and watch files light up; arrow-key through sessions.
2. **"Vibe" tags** — let users name a session ("Berlin trip") so future searches can use the label, stored locally.
3. **Co-occurrence boosts** — learn that certain folders/extensions cluster (downloads + invoices) and use it in ranking.
4. **Screenshot/photo thumbnails** in the TUI for instant visual recall.
5. **`back-then dupes`** — surface near-duplicate files within a session (same size + close time).
6. **Browser-download correlation** — optionally read browser history (local DB) to map a download to the page/time it came from.
7. **Natural-time grammar expansion** — "the Friday before Thanksgiving," "the week I started the new job" (with user-defined anchors).
8. **Shell integration** — `cd` into the folder of a result; pipe results into `fzf`.
9. **Cross-volume / external-drive indexing** with per-volume index files you can carry.
10. **`back-then forget <range>`** — prune the index for privacy/space.
11. **Plaintext/Markdown content peek** — optional opt-in snippet preview for text files in results.
12. **Export a session** as a zip or a dated folder ("give me everything from that week").

## 9. Out of scope

- **No cloud, no sync, no account, no telemetry.** Ever. That's the whole pitch.
- **No full-text content indexing** of arbitrary documents in v1 (that's Recoll's job; we're the "I don't remember the contents" tool). Optional opt-in peek is backlog only.
- **No LLM/embedding dependency** in core — it must run instantly and offline on a potato. (An *optional* local-model enricher could be a far-future plugin, not core.)
- **No file modification/moving/deleting** beyond the explicit `forget`/`export` commands — `back-then` is read-mostly and never reorganizes your disk for you.
- **No mobile app, no web UI** for v1. CLI/TUI on the desktop, period.
