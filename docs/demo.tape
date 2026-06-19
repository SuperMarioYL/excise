# vhs tape — Excise happy path.
# Render in CI via .github/workflows/demo.yml, or locally:
#   vhs docs/demo.tape   (needs the `excise` binary on PATH)
# vhs install: https://github.com/charmbracelet/vhs
Output assets/demo.gif

Set Shell "bash"
Set FontFamily "JetBrains Mono"
Set FontSize 16
Set Width 1200
Set Height 720
Set Theme "Catppuccin Mocha"
Set Padding 18
Set TypingSpeed 28ms

Type "# 1) Inspect a poisoned Claude Code session — read-only"
Enter
Sleep 600ms
Type "excise list testdata/claude_session_polluted.jsonl"
Enter
Sleep 2.5s

Type "# 2) Let the heuristic engine nominate the turns to cut"
Enter
Sleep 600ms
Type "excise suggest --top=5 testdata/claude_session_polluted.jsonl"
Enter
Sleep 3s

Type "# 3) Cut a range — snapshot first, then atomic write (dry-run here)"
Enter
Sleep 600ms
Type "excise cut 17,19,32 --dry-run testdata/claude_session_polluted.jsonl"
Enter
Sleep 3s
