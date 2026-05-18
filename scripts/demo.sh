#!/usr/bin/env bash
# Drives an asciinema recording. Used by scripts/regenerate_demo.sh.
set -euo pipefail

# Use a known fixture so the recording is deterministic.
EXCISE_BIN="${EXCISE_BIN:-./excise}"
FIXTURE="${FIXTURE:-testdata/claude_session_with_tools.jsonl}"

printf '\n$ excise list %s\n' "$FIXTURE"
"$EXCISE_BIN" list "$FIXTURE"

printf '\n$ excise cut 5-7 --dry-run %s\n' "$FIXTURE"
"$EXCISE_BIN" cut 5-7 --dry-run "$FIXTURE"

printf '\n$ excise rollback --list\n'
"$EXCISE_BIN" rollback --list || true
