# Changelog

All notable changes to **Excise** are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.0] — 2026-05-18

Adds the heuristic **suggestion engine** — Excise now nominates the top-K
candidate turns to excise from a polluted session so users reviewing a
200-turn transcript don't have to scroll all 200 to find the 3 that should die.

The v0.1 trust premise is preserved: pure stdlib, zero network, no LLM, no
auto-cut. The scorer only suggests; the user still confirms in the TUI or
via `excise cut <range>`.

### Added — m4 (heuristic suggestion engine)

- New package `internal/suggest`:
  - `Score(*session.Session) []TurnScore` — pure function, no state crosses
    sessions, no learning.
  - Five heuristic triggers: `high_token_cost`, `repeated_file_edit`,
    `user_correction_follows_up`, `tool_use_error_then_correction`,
    `long_drift_no_tool_calls`.
  - Hand-curated bilingual (en + zh-CN) `CorrectionLexicon` with per-phrase
    weights.
- New subcommand `excise suggest [path]`:
  - Default top-5 candidates; `--top=N`, `--min-score=X`, `--json` flags.
  - Read-only — never modifies the file.
  - Same auto-discovery path as `excise pick`; respects `--tool=`.
- `excise pick` now pre-marks the top-K candidates in the TUI by default.
  Pre-marked turns render with a `[◆]` glyph (amber in the bubbletea
  driver) plus a "◆ suggested — press space to uncheck" legend. The
  user can still toggle freely; commit only honors the final `Marked`
  set. `--no-suggest` restores v0.1 behavior.
- Two new polluted-session fixtures (`testdata/claude_session_polluted.jsonl`
  and `testdata/cursor_session_polluted.jsonl`) — 47 turns each, with
  planted failures the scorer is asserted to surface in its top-5.
- Bilingual README updates (en + zh-CN) covering the new command and
  the example table from the plan §1.

### Not changed (deliberately)

- No new external dependencies. Pure stdlib + the v0.1 trio of bubbletea /
  lipgloss / cobra.
- `internal/session/*` untouched — same schema, same writer, same atomic
  guarantees.
- Trust premise (`zero network`, `no LLM`, `no auto-cut`) — unchanged.

[0.2.0]: https://github.com/SuperMarioYL/excise/releases/tag/v0.2.0

## [0.1.0] — 2026-05-18

The first publicly demoable release.

### Added — m1 (Claude Code support)

- `excise list [--session=<path>]` — render a turn table from a Claude Code JSONL
  session, with role, timestamp, token estimate, and a content preview.
- `excise pick` — interactive bubbletea TUI to mark turns for surgical removal,
  with a live "tokens before → after" header.
- `excise cut <range>` — non-interactive cut for scripted use
  (e.g. `excise cut 5-7,9`).
- Auto-discovery of the newest session under `~/.claude/projects/<dir-encoded-cwd>/`.
- Tool-call ↔ tool-result dependency tracking: excising a turn that owns
  `tool_use` blocks automatically removes the matching `tool_result` blocks, so
  the next assistant turn does not reference a phantom tool call.

### Added — m2 (Cursor support)

- Cursor `state.vscdb` reader (`~/Library/Application Support/Cursor/User/globalStorage/state.vscdb`)
  that decodes `bubbleId:<composerId>:<bubbleId>` rows into the same internal
  `Turn` shape as Claude Code sessions.
- Auto-detection via `--tool=auto|claude|cursor`; sniffs the input path.
- Graceful "no Cursor data found" path with a fixture-only unit test for users
  who do not have Cursor installed.

### Added — m3 (safety + rollback)

- Snapshot-before-write — every commit copies the original to
  `~/.excise/snapshots/<session-id>/<iso-timestamp>.jsonl.gz`.
- `excise rollback --list` and `excise rollback <snapshot-id>`.
- 30-day snapshot auto-prune on each commit.
- Append-only `edit_log.jsonl` records every excised turn (who/what/when).
- Dependency-aware refuse-or-warn when a cut would orphan a downstream tool
  reference.

### Added — release engineering

- Cross-platform CI (`.github/workflows/ci.yml`) running `go vet`, `go build`,
  `go test ./...` on Linux + macOS for Go 1.24.
- Bilingual README (en + zh-CN), shields.io badges, animated capsule-render
  header, and a 30-second asciinema demo above the fold.
- `BUILD_SETUP_NEXT_STEPS.md` covering one-time release setup
  (asciinema re-record, Homebrew tap, AUR PKGBUILD, `go install` publish).

[0.1.0]: https://github.com/SuperMarioYL/excise/releases/tag/v0.1.0
