# Changelog

All notable changes to **Excise** are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.4.0] — 2026-06-20

### Added
- **Opt-in remote LLM rerank backend** (OpenAI / Anthropic / OpenRouter).
  Lifts the v0.3 deferral. New `internal/llm/remote.go` implements the existing
  `Reranker` interface (a new package member, not a refactor), so the rerank
  call site is unchanged — only transport + auth differ from the Ollama path.
- `excise.toml` `[llm]` block gains `backend` (`ollama` | `remote`), `provider`
  (`openai` | `anthropic` | `openrouter`), `api_key` / `api_key_env`, and
  `base_url`. New CLI overrides `--llm-backend` / `--llm-provider`.
- New testdata `claude_session_remote_rerank.jsonl` + a stubbed-remote test
  suite (`internal/llm/remote_test.go`) covering the OpenAI and Anthropic wire
  shapes, the auth-failure / missing-key / timeout fallbacks, and the stderr
  host echo. Live remote test gated behind `EXCISE_LIVE_REMOTE=1` (CI never
  calls a real provider).

### Changed
- **Trust contract preserved by design.** The default backend stays `ollama`
  (local-only); the remote path is reached only when `backend=remote` AND a key
  is supplied, and the destination host is echoed to stderr on every remote
  call so an outbound call is never silent. Same graceful fallback as the
  Ollama path: on auth failure / timeout / unreachable, warn on stderr and fall
  back to the heuristic ranking (exit 0).

### Fixed
- **`[llm].top_n` is now honored.** The documented knob was loaded, defaulted,
  validated and documented but never read — `rankCandidates` sized the LLM
  shortlist from the CLI `--top` arg only, making `top_n` a silent no-op. It now
  governs the LLM shortlist (`max(top, top_n)`).
- **Previews truncate on rune boundaries, not byte offsets.** `previewText`
  (`internal/session/loader.go`), `printList` (`cmd/excise/main.go`), and the
  TUI `truncate` (`internal/tui/model.go`) byte-sliced UTF-8 strings, which
  could split a multibyte rune (common for zh-CN sessions) and emit invalid
  UTF-8 into both the display and the Ollama rerank prompt. All three now cap by
  rune count.
- **Cursor sqlite cuts no longer risk clobbering the live database on
  rollback.** A Cursor `.vscdb` cut is non-destructive (it only writes a sidecar
  `<db>.excised.jsonl`), but `commitExcise` snapshotted/logged the live `.vscdb`
  as `source_path`, so `excise rollback <id>` could rename a stale snapshot over
  the user's current Cursor database. Snapshot + edit-log `source_path` now
  resolve via `session.WriteTarget` to the actual write target (the sidecar),
  never the live DB; first sidecar cuts (nothing to roll back to) skip the
  snapshot.

## [0.3.0] — 2026-05-23

### Added
- **Opt-in LLM rerank via local Ollama** (`--llm`). Layers a local model on
  top of the v0.2 heuristic shortlist: heuristic stays the cheap pre-filter,
  LLM only judges that shortlist and writes a one-line reason per turn.
- New flags `--llm`, `--llm-model`, `--llm-host` on both `suggest` and `pick`.
- New optional config file `excise.toml` (`./excise.toml` → `$XDG_CONFIG_HOME/excise/`
  → `~/.config/excise/`) with `[llm]` block: `host`, `model`, `top_n`, `timeout_sec`.
- `internal/llm/` package (tiny Ollama HTTP client + rerank logic + hard-coded
  prompt template) and `internal/config/` package (TOML loader with sane defaults).
- New testdata `claude_session_llm_rerank.jsonl` + cmd-level tests for the
  happy path, fallback path, and JSON-parse-failure path against a stubbed
  `Reranker`. Live-Ollama integration test gated behind `EXCISE_LIVE_OLLAMA=1`.

### Changed
- `suggest` and `pick` route the heuristic shortlist through the LLM reranker
  when `--llm` is set. Exit codes unchanged: 0 on success (incl. graceful
  fallback), 2 only on usage error.
- TUI shows per-turn `LLMReason` next to pre-marked turns when present.

### Fixed
- Test helper `NewStubReranker` was originally placed in `_test.go` (invisible
  across packages); moved to `internal/llm/stub.go` so cmd-level tests resolve.
- `go.sum` regenerated to match real `BurntSushi/toml@v1.4.0` checksum.

### Trust contract (unchanged from v0.1/v0.2)
- No outbound network beyond the user-configured Ollama host.
- No autocut. `--llm` only changes ranking; user still confirms every excision.
- No telemetry, no acceptance log, no cross-session learning.

### Still out of scope
- Remote LLM API key backends (OpenAI / Anthropic / OpenRouter) — deferred to v0.4.
- Streaming LLM output / partial-rerank UI.
- LLM + autocut combination.
- User-editable prompt template.

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
