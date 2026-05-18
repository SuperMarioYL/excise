#!/usr/bin/env bash
# Re-record docs/demo.cast end-to-end.
# Requires: asciinema (`brew install asciinema`) and optionally vhs.
set -euo pipefail
HERE="$(cd "$(dirname "$0")/.." && pwd)"
cd "$HERE"
go build -o ./excise ./cmd/excise
asciinema rec --overwrite --idle-time-limit 1.5 --command "bash scripts/demo.sh" docs/demo.cast
echo "demo.cast updated; commit and push."
