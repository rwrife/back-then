# back-then 🕰️

**A local-first time machine for your files.**

You don't remember the filename. You remember *roughly when* it happened — "that PDF from around the Berlin trip," "the spreadsheet from tax season," "whatever I downloaded the day everything broke." `back-then` finds it by **time and circumstance**, not by name.

- 🔌 **100% offline.** No cloud, no account, no telemetry. Your files never leave the machine.
- 🧠 **Episodic search.** Reconstructs the *sessions* of files that arrived together, so you can browse time instead of folders.
- ⚡ **Fast & tiny.** A single Go binary backed by a local SQLite index. Runs on a potato.
- 🖥️ **Cross-platform.** Windows, macOS, Linux.

> ⚠️ Early days — see [PLAN.md](./PLAN.md) for the roadmap. M1 is scaffolding.

## Quickstart (planned)

```sh
# Build an index of where your stuff lives (local SQLite, incremental)
back-then index ~/Downloads ~/Documents

# Find by fuzzy time
back-then find "around last spring"
back-then find "the week of jun 3"

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
