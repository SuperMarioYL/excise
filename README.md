<!-- LANGUAGE: [English](README.md) | [简体中文](README.zh-CN.md) -->

<p align="center">
  <img src="https://capsule-render.vercel.app/api?type=waving&color=0:1e3a8a,100:7c3aed&height=170&section=header&text=Excise&fontColor=ffffff&fontSize=64&fontAlignY=38&desc=Surgical%20context%20editing%20for%20coding-agent%20sessions&descAlignY=66&descSize=16" alt="Excise — surgical context editing for coding-agent sessions"/>
</p>

<p align="center">
  <a href="https://github.com/SuperMarioYL/excise/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/SuperMarioYL/excise/ci.yml?branch=main&label=CI&logo=github&style=flat-square"/></a>
  <a href="https://golang.org/dl/"><img alt="Go" src="https://img.shields.io/badge/go-1.24%2B-00ADD8?logo=go&style=flat-square"/></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-MIT-blue?style=flat-square"/></a>
  <a href="https://github.com/SuperMarioYL/excise/releases"><img alt="Release" src="https://img.shields.io/github/v/release/SuperMarioYL/excise?display_name=tag&logo=github&style=flat-square"/></a>
  <a href="https://github.com/SuperMarioYL/excise/stargazers"><img alt="Stars" src="https://img.shields.io/github/stars/SuperMarioYL/excise?style=flat-square&logo=github&color=yellow"/></a>
  <a href="https://goreportcard.com/report/github.com/SuperMarioYL/excise"><img alt="Go report" src="https://goreportcard.com/badge/github.com/SuperMarioYL/excise?style=flat-square"/></a>
</p>

<p align="center">
  <img src="https://readme-typing-svg.demolab.com?font=JetBrains+Mono&weight=600&size=20&duration=2800&pause=900&color=7c3aed&center=true&vCenter=true&width=720&lines=The+missing+primitive+between+%2Fclear+and+%2Fcompact;Cut+poisoned+turns+without+restarting+the+session;Tool_use+%E2%86%94+tool_result+pairs+travel+together;Snapshot-first.+Undo+is+one+command." alt="tagline"/>
</p>

<p align="center">
  <a href="https://asciinema.org/a/ASCIINEMA-ID">
    <img src="https://asciinema.org/a/ASCIINEMA-ID.svg" alt="30-second demo — three poisoned turns excised from a real Claude Code session" width="780"/>
  </a>
  <br/>
  <em>30-second demo · click to play · or see <code>docs/demo.cast</code> in this repo</em>
</p>

---

## What is Excise?

Anthropic's own documentation observes that more than two corrections in a
single Claude Code session reliably poisons the trajectory — the agent starts
fighting ghosts of the cut-off path you abandoned. The fix today is
`/clear` (lose all context) or `/compact` (lose nuance). **Excise** is the
missing third option: a single-binary CLI that opens an interactive picker
over the session JSONL on disk, lets you cut the three turns that took the
agent down the dead end, and writes the file back with snapshot-and-undo
safety.

```
        before              after
    ┌──────────────┐    ┌──────────────┐
    │ user         │    │ user         │
    │ assistant    │    │ assistant    │
    │ user         │ ─▶ │   (excised)  │
    │ assistant ✗  │    │   (excised)  │
    │ user         │    │ user         │
    │ assistant ✓  │    │ assistant ✓  │
    └──────────────┘    └──────────────┘
```

The primitive `Excise(Session, set<turn_id>) -> Session'` preserves four
invariants: **(1)** removing a `tool_use` turn also removes its paired
`tool_result`, **(2)** removing a `tool_result` warns about the surviving
owner, **(3)** ordering and stable ids are preserved, **(4)** writes are
atomic (snapshot, write-to-tmp, rename).

## Table of contents

- [Install](#install)
- [Quick start](#quick-start)
- [v0.2 — Suggestions](#v02--suggestions)
- [Commands](#commands)
- [How dependency-aware cutting works](#how-dependency-aware-cutting-works)
- [Snapshots and rollback](#snapshots-and-rollback)
- [Supported transcript formats](#supported-transcript-formats)
- [Architecture](#architecture)
- [Roadmap](#roadmap)
- [Out of scope (on purpose)](#out-of-scope-on-purpose)
- [Contributing](#contributing)
- [License](#license)

## Install

```bash
# via go
go install github.com/SuperMarioYL/excise/cmd/excise@latest

# via homebrew (after the tap is published — see BUILD_SETUP_NEXT_STEPS.md)
brew install supermarioyl/tap/excise

# from source
git clone https://github.com/SuperMarioYL/excise && cd excise
go build -o ./excise ./cmd/excise
```

Excise ships as a single ~8 MB static binary. No runtime, no daemon, no
network call.

## Quick start

```bash
# Zero-arg: auto-discover the newest Claude Code session and open the picker
excise

# Render the turn table, no edits
excise list

# Open the picker on a specific session file
excise pick ~/.claude/projects/-home-me-app/SESSION-UUID.jsonl

# Non-interactive cut: remove turns 5-7 and 9
excise cut 5-7,9 ~/.claude/projects/-home-me-app/SESSION-UUID.jsonl

# Same, but for a Cursor session
excise --tool=cursor cut 12-14 "~/Library/Application Support/Cursor/User/globalStorage/state.vscdb"

# Restore the most recent snapshot
excise rollback --list
excise rollback <snapshot-id>
```

In the TUI:

| key | action |
| --- | --- |
| `j` / `↓` | move down |
| `k` / `↑` | move up |
| `g` / `G` | jump to top / bottom |
| `space` / `x` | toggle mark on current turn |
| `d` | mark + move down |
| `enter` | commit the cut |
| `q` / `ctrl+c` | abort without changes |

The header live-updates `turns: 42 → 39   tokens: ~18.2k → ~12.4k` as you mark.

## v0.2 — Suggestions

`excise suggest` runs a **pure-stdlib heuristic scorer** over the session and
nominates the top-K candidate turns to excise. Zero network, no LLM, no
auto-cut — the scorer only suggests; you still confirm in the TUI.

```text
 #   role        tokens   heuristic                                        preview
---  ---------   ------   ----------------------------------------------   ----------------------
 17  assistant     2840   high_token_cost + user_correction_follows_up     "Let me try refactoring …"
 19  tool_use       420   tool_use_error_then_correction                   Edit(path=foo, …) ERROR
 32  assistant     3100   high_token_cost + repeated_file_edit             "Actually let me revert …"
 33  assistant     1820   repeated_file_edit + user_correction_follows_up  "I'll switch to using …"
 47  assistant     2200   long_drift_no_tool_calls + high_token_cost       "To summarize what we …"

5 candidates totalling ~10,380 tokens. Run `excise pick` to review interactively.
```

Five heuristics contribute to each turn's score:

| trigger | what it fires on |
| --- | --- |
| `high_token_cost` | assistant or tool turn weighing ≥ 2 000 tokens |
| `repeated_file_edit` | same file edited 3+ times in a row (window = 3) |
| `user_correction_follows_up` | next user turn matches the **bilingual** correction lexicon (`no`, `actually`, `try a different approach`, `不对`, `换个思路` …) |
| `tool_use_error_then_correction` | a tool returned an error AND the next user turn said so |
| `long_drift_no_tool_calls` | 5+ consecutive assistant turns with no `tool_use` |

`excise pick` calls the scorer **by default** and pre-marks the top-K
candidates in the TUI — those marks render with a `[◆]` glyph instead of
the manual `[x]`. Toggle freely with `space`; commit only honors your final
marks. Pass `--no-suggest` to restore v0.1 behavior.

```bash
# Dry-run the suggestion engine — never touches the file
excise suggest

# Top-3, only above score 1.5, JSON output
excise suggest --top=3 --min-score=1.5 --json testdata/claude_session_polluted.jsonl

# Skip the pre-mark — v0.1 picker behavior
excise pick --no-suggest
```

The scorer is a **pure function of one session**: no cross-session learning,
no acceptance log written, no shared cache. That keeps the trust premise
intact and means a v0.2 binary on an air-gapped machine behaves identically
to one online.

## Commands

```
excise [path]                      open the TUI (auto-discover if no path; pre-mark via heuristics)
excise list [path]                 print the turn table (no edits)
excise pick [path]                 open the TUI (alias for the bare command)
excise cut <range> [path]          non-interactive; e.g. "5-7,9"
excise suggest [path]              v0.2 — print top-K heuristic candidates (read-only)
excise rollback --list             list every snapshot, newest first
excise rollback <snapshot-id>      restore one snapshot

global flags:
  --tool       auto|claude|cursor   transcript format (default: auto)
  --session    PATH                 explicit session path
  --force                           proceed despite dependency warnings
  --dry-run                         show the diff but do not write
  -y, --yes                         skip the confirmation prompt
  --no-suggest                      v0.2 — skip the heuristic pre-mark in the picker

suggest-only flags:
  --top N                           show at most N suggestions (0 = all; default 5)
  --min-score X                     drop suggestions below score X (default 0)
  --json                            emit JSON instead of a table
```

## How dependency-aware cutting works

A Claude Code (or Cursor) turn that issues a tool call creates a downstream
`tool_result` turn. If you cut the tool call but leave its result behind, the
agent's next turn references a `tool_use_id` that no longer exists and the
model gets confused.

Excise builds a dependency graph from every `tool_use.id` ↔ `tool_result.tool_use_id`
edge, computes the transitive closure of your marked set, and (a) pulls every
dependent result into the cut automatically (invariant 1), and (b) warns when
your selection would orphan a `tool_result` whose owner survives (invariant 2).
Pass `--force` to override the warning.

## Snapshots and rollback

Every commit copies the source file to:

```
~/.excise/snapshots/<session-id>/<rfc3339-timestamp>.jsonl.gz
```

and appends a JSON line to `~/.excise/edit_log.jsonl` recording what was
removed, when, and why. Snapshots older than 30 days are auto-pruned on the
next commit so the directory stays bounded.

`excise rollback <snapshot-id>` restores byte-for-byte. The original
destination is read from the edit log; override with `--to <path>` if you
moved the session file.

## Supported transcript formats

| Tool | Path | Read | Write |
| --- | --- | --- | --- |
| **Claude Code** | `~/.claude/projects/<dir>/<uuid>.jsonl` | yes | atomic rewrite of the original |
| **Cursor** | `~/Library/Application Support/Cursor/User/globalStorage/state.vscdb` | yes (via `sqlite3` CLI, read-only) | side-car `.excised.jsonl` to avoid corrupting an open db |
| **Cursor (fixture export)** | `*.jsonl` of `{"composerId":...,"bubble":{...}}` lines | yes | atomic rewrite |

`sqlite3` is required only for the Cursor sqlite branch. macOS ships it; on
Linux: `apt install sqlite3`. The Cursor writer deliberately refuses to mutate
`state.vscdb` while Cursor is running — v0.2 will add a "Cursor must be
closed" guard and a direct write path.

## Architecture

```
┌────────────────────────────┐
│  cmd/excise/main.go        │  cobra CLI: excise [pick|list|cut|rollback]
└────────────┬───────────────┘
             │
   ┌─────────┴────────────────────────────┐
   ▼                                      ▼
internal/session                     internal/tui
  loader.go      Tool / Turn model     model.go   pure state + token math
  claude.go      Claude JSONL          picker.go  hand-rolled stdin driver
  cursor.go      Cursor sqlite+jsonl   bubbletea.go  real terminal UI
  dependency.go  tool-call graph       diff.go    before/after summary
  writer.go      WriterFor() dispatch
                    │
                    ▼
            internal/safety
              backup.go    snapshot + edit_log + rollback
```

One binary, no daemon, no IPC.

## Roadmap

- **v0.2** ✅ — heuristic suggestion engine + TUI pre-mark (this release).
- **v0.3** — direct `state.vscdb` writes with a "Cursor closed?" guard; opt-in
  LLM-assisted suggestions as a plugin (local Ollama / user-supplied API key).
- **v0.4** — Codex / Aider / Cline support behind the same primitive.
- **v0.5** — `excise grep <regex>` to mark by content match.
- **v0.6** — a "session debugger" sidecar that lets you inspect tool-call
  graphs without cutting anything.

## Out of scope (on purpose)

- Web UI or hosted service. CLI/TUI only.
- Auto-**cutting** turns. v0.2 only **suggests** (pure-stdlib heuristics,
  zero network); you still press `enter` to commit. `excise autocut` is
  explicitly never going to ship.
- LLM-assisted suggestions of any kind (local Ollama / user-supplied API
  key / hosted endpoint). All deferred to v0.3 as opt-in plugins so the
  v0.2 binary signature and trust premise stay put.
- Prompt-cache reconciliation. Editing invalidates the cache; you accept the
  cost.
- A Claude Code plugin. We operate on the on-disk file between sessions.
- Cloud sync, account system, team features.
- Cross-tool session portability — that is [cli-continues](https://github.com/cli-continues/cli-continues)'
  niche, not ours.
- Telemetry of any kind, including opt-in.

Every refusal above keeps the demo under 30 seconds.

## Contributing

Bug reports and PRs welcome. Please run `go test -race ./...` before
submitting. The hot path you most likely want to change is
`internal/session/claude.go` (schema tolerance) or `internal/safety/backup.go`
(snapshot policy). Anything bigger, open an issue first so we can agree on
scope.

## License

[MIT](LICENSE) © 2026 SuperMarioYL
