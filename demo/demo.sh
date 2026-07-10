#!/usr/bin/env bash
#
# demo.sh — a self-contained, reproducible tour of back-then.
#
# It builds the binary, conjures a small synthetic file tree with realistic
# (back-dated) timestamps, then walks through the headline commands:
#   index → stats → sessions → find → near
#
# Nothing here touches your real files or your real index — everything lives
# in a throwaway temp directory that is cleaned up on exit. Run it yourself:
#
#   ./demo/demo.sh
#
# Or record it as an asciicast:
#
#   asciinema rec --overwrite --command ./demo/demo.sh demo/back-then.cast
#
set -euo pipefail

# --- locate repo root regardless of where we're invoked from ----------------
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." >/dev/null 2>&1 && pwd)"
cd "${REPO_ROOT}"

# --- scratch space ----------------------------------------------------------
# We build the binary first, then hop into a throwaway working directory so the
# transcript shows short, friendly relative paths instead of long temp paths.
WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT
BT="${WORK}/back-then"
DB=".back-then.db"      # relative to the demo working dir (see cd below)
TREE="my-files"         # ditto

# --- tiny helpers for a readable "typed" transcript -------------------------
BOLD=$'\033[1m'; DIM=$'\033[2m'; CYAN=$'\033[36m'; RESET=$'\033[0m'
say()  { printf '\n%s# %s%s\n' "${DIM}" "$*" "${RESET}"; }
run()  { printf '%s$ %s%s\n' "${CYAN}${BOLD}" "$*" "${RESET}"; eval "$*"; }
pause(){ sleep "${DEMO_PAUSE:-1.1}"; }

# --- backdate helper: set mtime to "N days ago" -----------------------------
# Uses GNU date's -d; falls back to BSD date's -v on macOS.
backdate() {
  local target="$1" days_ago="$2" ts
  if ts="$(date -d "${days_ago} days ago" +%Y%m%d%H%M 2>/dev/null)"; then
    :
  else
    ts="$(date -v-"${days_ago}"d +%Y%m%d%H%M)"
  fi
  touch -t "${ts}" "${target}"
}

# ---------------------------------------------------------------------------
say "build the binary (single static Go binary, no runtime needed)"
run "go build -o '${BT}' ./cmd/back-then"
pause

# From here on we work inside the scratch dir so paths stay short & readable.
# A shell function lets the transcript show a clean `back-then ...` command
# while still invoking the freshly built binary.
cd "${WORK}"
back-then() { "${BT}" "$@"; }

say "here's a little world: three bursts of files from different moments"
mkdir -p \
  "${TREE}/berlin-trip" \
  "${TREE}/taxes-2025" \
  "${TREE}/downloads"

# --- Session A: "Berlin trip", ~1 year ago (a spring burst) ----------------
for f in itinerary.pdf boarding-pass.pdf hotel.pdf IMG_2043.jpg IMG_2051.jpg; do
  echo "berlin: ${f}" > "${TREE}/berlin-trip/${f}"
  backdate "${TREE}/berlin-trip/${f}" 400
done

# --- Session B: "tax season", ~4 months ago --------------------------------
for f in w2.pdf 1099.pdf deductions.xlsx receipts.zip; do
  echo "taxes: ${f}" > "${TREE}/taxes-2025/${f}"
  backdate "${TREE}/taxes-2025/${f}" 120
done

# --- Session C: "the day everything broke", ~3 days ago --------------------
for f in stacktrace.log config.bak db-dump.sql notes.md; do
  echo "incident: ${f}" > "${TREE}/downloads/${f}"
  backdate "${TREE}/downloads/${f}" 3
done

run "find ${TREE} -type f | sort"
pause

say "index it — incremental, offline, into a local SQLite file"
run "back-then --db ${DB} index ${TREE}"
pause

say "what's in the index?"
run "back-then --db ${DB} stats"
pause

say "browse TIME, not folders: reconstructed sessions (bursts of files)"
run "back-then --db ${DB} sessions"
pause

say "you don't recall the name — just 'that stuff from around tax season'"
run "back-then --db ${DB} find 'around 4 months ago'"
pause

say "the killer move: what arrived *near* a file you DO remember?"
run "back-then --db ${DB} near ${TREE}/berlin-trip/itinerary.pdf --window 48h"
pause

say "that's back-then: find files by when they happened, 100% offline."
