<!-- 语言: [English](README.md) | [简体中文](README.zh-CN.md) -->

<p align="center">
  <img src="https://capsule-render.vercel.app/api?type=waving&color=0:1e3a8a,100:7c3aed&height=170&section=header&text=Excise&fontColor=ffffff&fontSize=64&fontAlignY=38&desc=%E4%B8%BA%E7%BC%96%E7%A0%81%20Agent%20%E4%BC%9A%E8%AF%9D%E5%81%9A%E5%A4%96%E7%A7%91%E6%89%8B%E6%9C%AF&descAlignY=66&descSize=16" alt="Excise — 为编码 Agent 会话做外科手术"/>
</p>

<p align="center">
  <a href="https://github.com/SuperMarioYL/excise/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/SuperMarioYL/excise/ci.yml?branch=main&label=CI&logo=github&style=flat-square"/></a>
  <a href="https://golang.org/dl/"><img alt="Go" src="https://img.shields.io/badge/go-1.24%2B-00ADD8?logo=go&style=flat-square"/></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-MIT-blue?style=flat-square"/></a>
  <a href="https://github.com/SuperMarioYL/excise/releases"><img alt="Release" src="https://img.shields.io/github/v/release/SuperMarioYL/excise?display_name=tag&logo=github&style=flat-square"/></a>
  <a href="https://github.com/SuperMarioYL/excise/stargazers"><img alt="Stars" src="https://img.shields.io/github/stars/SuperMarioYL/excise?style=flat-square&logo=github&color=yellow"/></a>
</p>

<p align="center">
  <img src="https://readme-typing-svg.demolab.com?font=JetBrains+Mono&weight=600&size=20&duration=2800&pause=900&color=7c3aed&center=true&vCenter=true&width=760&lines=%2Fclear+%E5%92%8C+%2Fcompact+%E4%B9%8B%E9%97%B4%E7%9A%84%E7%BC%BA%E5%A4%B1%E5%8E%9F%E8%AF%AD;%E5%A4%96%E7%A7%91%E6%89%8B%E6%9C%AF%E5%88%87%E9%99%A4%E8%A2%AB%E6%B1%A1%E6%9F%93%E7%9A%84%E5%9B%9E%E5%90%88;tool_use+%E2%86%94+tool_result+%E6%88%90%E5%AF%B9%E8%B5%B0%EF%BC%8C%E4%B8%8D%E7%95%99%E5%AD%A4%E5%84%BF;%E5%85%88%E5%BF%AB%E7%85%A7%EF%BC%8C%E5%86%8D%E5%86%99%E5%85%A5%EF%BC%8C%E4%B8%80%E6%9D%A1%E5%91%BD%E4%BB%A4%E5%9B%9E%E6%BB%9A" alt="标语"/>
</p>

<p align="center">
  <a href="https://asciinema.org/a/ASCIINEMA-ID">
    <img src="https://asciinema.org/a/ASCIINEMA-ID.svg" alt="30 秒演示 — 把一个真实 Claude Code 会话里的三个污染回合切掉" width="780"/>
  </a>
  <br/>
  <em>30 秒演示 · 点击播放 · 也可见仓库内的 <code>docs/demo.cast</code></em>
</p>

---

## Excise 是什么？

Anthropic 自己的文档观察到：在同一个 Claude Code 会话里，**超过两次更正**就会
可靠地把对话引入死路 —— 模型会一直纠结于你已经放弃的那条岔路。今天唯一的
解药是 `/clear`（丢掉所有上下文）或 `/compact`（丢掉细节）。**Excise** 是
缺失的第三个选项：一个单二进制 CLI，对磁盘上的会话 JSONL 文件开一个交互式
选择器，让你把那三个把 Agent 带歪的回合精准切除，并自带**快照 + 一键回滚**
的安全网。

```
        切除前              切除后
    ┌──────────────┐    ┌──────────────┐
    │ user         │    │ user         │
    │ assistant    │    │ assistant    │
    │ user         │ ─▶ │   (excised)  │
    │ assistant ✗  │    │   (excised)  │
    │ user         │    │ user         │
    │ assistant ✓  │    │ assistant ✓  │
    └──────────────┘    └──────────────┘
```

核心原语 `Excise(Session, set<turn_id>) -> Session'` 保持四条不变量：
**(1)** 切除一个含 `tool_use` 的回合时，配对的 `tool_result` 自动一起切；
**(2)** 切 `tool_result` 但保留它的 owner 时，会发出警告；
**(3)** 剩余回合的顺序与稳定 ID 不变；
**(4)** 写入是原子的（先快照、写 tmp 文件、再 rename）。

## 目录

- [安装](#安装)
- [快速开始](#快速开始)
- [v0.2 — 智能建议](#v02--智能建议)
- [命令一览](#命令一览)
- [依赖感知的切除是怎么做的](#依赖感知的切除是怎么做的)
- [快照与回滚](#快照与回滚)
- [支持的会话格式](#支持的会话格式)
- [架构](#架构)
- [路线图](#路线图)
- [明确不做的事](#明确不做的事)
- [贡献](#贡献)
- [许可证](#许可证)

## 安装

```bash
# 通过 go install
go install github.com/SuperMarioYL/excise/cmd/excise@latest

# 通过 Homebrew（tap 发布后可用，参考 BUILD_SETUP_NEXT_STEPS.md）
brew install supermarioyl/tap/excise

# 从源码编译
git clone https://github.com/SuperMarioYL/excise && cd excise
go build -o ./excise ./cmd/excise
```

Excise 是一个约 8 MB 的静态二进制：没有运行时，没有守护进程，不发任何网络
请求。

## 快速开始

```bash
# 零参数：自动找到最新的 Claude Code 会话并打开选择器
excise

# 只看回合列表，不做任何修改
excise list

# 在指定文件上打开选择器
excise pick ~/.claude/projects/-home-me-app/SESSION-UUID.jsonl

# 非交互式：删掉 5-7 和 9 这几个回合
excise cut 5-7,9 ~/.claude/projects/-home-me-app/SESSION-UUID.jsonl

# 同样的操作，但作用在 Cursor 会话上
excise --tool=cursor cut 12-14 "~/Library/Application Support/Cursor/User/globalStorage/state.vscdb"

# 列出所有快照 / 回滚到某个快照
excise rollback --list
excise rollback <snapshot-id>
```

TUI 内的按键映射：

| 按键 | 动作 |
| --- | --- |
| `j` / `↓` | 向下移动 |
| `k` / `↑` | 向上移动 |
| `g` / `G` | 跳到第一个 / 最后一个回合 |
| `space` / `x` | 标记 / 取消标记当前回合 |
| `d` | 标记当前并向下移动 |
| `enter` | 提交切除 |
| `q` / `ctrl+c` | 不保存退出 |

头部会实时更新 `turns: 42 → 39   tokens: ~18.2k → ~12.4k`，让你边标边看
省了多少 token。

## v0.2 — 智能建议

`excise suggest` 在会话上跑一个**纯标准库的启发式打分器**，告诉你"如果只
能切 K 个回合，最该切的是哪几个"。零网络、不用 LLM、不会自动剪切 —— 打分
器只**建议**，最终是否提交仍由你在 TUI 里确认。

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

5 个启发式触发器，按权重相加：

| 触发器 | 命中条件 |
| --- | --- |
| `high_token_cost` | assistant 或 tool 回合 ≥ 2 000 token |
| `repeated_file_edit` | 同一文件被连续 edit 3+ 次（窗口 = 3） |
| `user_correction_follows_up` | 紧接着的 user 回合命中**双语**纠错词典（`no` / `actually` / `try a different approach` / `不对` / `换个思路` …） |
| `tool_use_error_then_correction` | 工具调用返回了 error，下一个 user 回合恰好说了"错了" |
| `long_drift_no_tool_calls` | 连续 5+ 个 assistant 回合没有任何 `tool_use` |

`excise pick` 默认会调一次打分器，把 top-K 自动**预标记**进 TUI——预标
记的回合用 `[◆]` 而不是手动的 `[x]` 显示。你可以按 `space` 自由勾选/取消；
提交时只看你最后的勾选状态。加 `--no-suggest` 关闭这个行为，回到 v0.1。

```bash
# 跑一遍建议，不写文件
excise suggest

# 只看 top-3、分数 ≥ 1.5、用 JSON 输出
excise suggest --top=3 --min-score=1.5 --json testdata/claude_session_polluted.jsonl

# 关闭预标记，恢复 v0.1 行为
excise pick --no-suggest
```

打分器是一个**只看单一会话**的纯函数：不做跨会话学习、不记录你接受了哪些
建议、也不维护共享缓存。这样 v0.2 的二进制在断网机器上的行为，和联网时
一模一样。

## v0.3 — LLM 重排（可选，仅本地 Ollama）

v0.3 让你**可选**在 v0.2 的启发式短名单上再叠一层本地 Ollama 模型。启发式
仍然是便宜的预筛；LLM 只对短名单做重排并给每条 turn 写一句理由。默认行为
和 v0.2 完全一样——`--llm` 是唯一开关。

```bash
# 装 Ollama（https://ollama.com），拉一个小模型：
ollama pull llama3.2

# 只在本次跑里启用 LLM：
excise suggest --llm session.jsonl
excise pick --llm session.jsonl     # TUI 会在每条预标记的 turn 旁边显示 LLM 理由
```

可选的 `excise.toml`（发现路径：`./excise.toml` → `$XDG_CONFIG_HOME/excise/excise.toml`
→ `~/.config/excise/excise.toml`）：

```toml
[llm]
host = "http://localhost:11434"   # 你自己的 Ollama
model = "llama3.2"
top_n = 5
timeout_sec = 20
```

**优雅降级。** 如果 Ollama 连不上、模型缺失、调用超时，或返回不合法，Excise
会在 stderr 打一行——

```
[excise] LLM unavailable (<原因>) — falling back to heuristic ranking
```

——然后用 v0.2 的启发式结果继续。`--llm` 写进肌肉记忆里也不会硬卡住你。

**`--llm` 下的信任契约**（与 v0.1/v0.2 一致）：

- **不发任何外网请求**，只发到你配置的 host。默认 `http://localhost:11434`
  是你自己机器的 Ollama；指向别处是你自己显式做的选择。
- **不 autocut。** `--llm` 只改变*排序*，每一条切除仍然要你在 TUI 里、或者
  显式 `excise cut <range>` 确认。
- **不打点、不记接受日志、不跨会话学习。** 立场和 v0.1/v0.2 完全一样。

### 远程后端（v0.4，按需开启）

从 v0.4 起，你可以把重排后端指向远程厂商，而非本地 Ollama。信任前提通过
**显式、按需开启**来保持：

- 默认后端仍是 `ollama`（仅本地）。只有当你设置 `backend = "remote"`
  **并且**提供了 key 时，才会发生远程调用。
- 每次远程调用都会把目标 host 打到 stderr —— 外网调用永远不会静默发生。
- 与 Ollama 路径相同的优雅降级：鉴权失败 / 超时 / host 不可达时，`excise`
  打印一行 stderr 警告并回退到启发式排序（退出码 0）。

```toml
[llm]
backend  = "remote"            # ollama（默认）| remote
provider = "openai"            # openai | anthropic | openrouter
model    = "gpt-4o-mini"
api_key_env = "OPENAI_API_KEY" # 推荐用环境变量，而非内联 api_key
# base_url = "https://api.openai.com"   # 可选：覆盖默认 host
```

CLI 覆盖项 `--llm-backend` 和 `--llm-provider` 优先级高于配置块。已内置一个
真实远程集成测试，但用 `EXCISE_LIVE_REMOTE=1` 门控（CI 永不调用真实厂商）。

## 命令一览

```
excise [path]                      打开 TUI（无参数则自动发现；默认带启发式预标记）
excise list [path]                 打印回合表，不修改
excise pick [path]                 显式打开 TUI（与裸命令同义）
excise cut <range> [path]          非交互式切除；如 "5-7,9"
excise suggest [path]              v0.2 —— 打印 top-K 启发式候选（只读）
excise rollback --list             列出所有快照，最新在前
excise rollback <snapshot-id>      恢复某个快照

全局参数：
  --tool       auto|claude|cursor   会话格式（默认 auto）
  --session    PATH                 显式指定会话路径
  --force                           即使有依赖警告也继续
  --dry-run                         只显示差异，不写文件
  -y, --yes                         跳过最终确认
  --no-suggest                      v0.2 —— TUI 里跳过启发式预标记
  --llm                             v0.3 —— 用本地 Ollama 对启发式短名单做重排
  --llm-model NAME                  v0.3 —— 覆盖 excise.toml 里的 model（如 llama3.2）
  --llm-host URL                    v0.3 —— 覆盖 excise.toml 里的 host

suggest 专属参数：
  --top N                           最多显示 N 个候选（0 = 全部；默认 5）
  --min-score X                     分数低于 X 的候选不显示（默认 0）
  --json                            用 JSON 输出，不用表格
```

## 依赖感知的切除是怎么做的

Claude Code（以及 Cursor）的一个回合里若发起了工具调用，会在下游产生一个
`tool_result` 回合。如果你只切掉了工具调用却把结果留下来，模型下一回合会
引用一个不存在的 `tool_use_id`，于是就开始胡言乱语。

Excise 用 `tool_use.id ↔ tool_result.tool_use_id` 把这层边构造成一张依赖图，
对你勾选的集合做一次传递闭包：
**(a)** 自动把所有受牵连的 `tool_result` 一起切（不变量 1）；
**(b)** 若你的选择会让某个 `tool_result` 的 owner 还活着，则发出警告
（不变量 2）。加 `--force` 可以强制覆盖警告。

## 快照与回滚

每次提交都会把原文件复制到：

```
~/.excise/snapshots/<session-id>/<rfc3339-timestamp>.jsonl.gz
```

并往 `~/.excise/edit_log.jsonl` 追加一行 JSON，记下切了什么、什么时候切的、
为什么切。超过 30 天的快照在下一次提交时自动清理，目录大小有界。

`excise rollback <snapshot-id>` 按字节恢复。原始路径从 edit_log 里读出来；
若你把会话文件挪过位置，用 `--to <path>` 显式指定恢复目标。

## 支持的会话格式

| 工具 | 路径 | 读 | 写 |
| --- | --- | --- | --- |
| **Claude Code** | `~/.claude/projects/<dir>/<uuid>.jsonl` | 支持 | 原文件原子覆盖 |
| **Cursor** | `~/Library/Application Support/Cursor/User/globalStorage/state.vscdb` | 支持（调 `sqlite3` CLI 只读） | 旁路 `.excised.jsonl`，避免与运行中的 Cursor 抢库 |
| **Cursor（fixture 导出）** | 每行 `{"composerId":...,"bubble":{...}}` 的 `*.jsonl` | 支持 | 原子覆盖 |

只有 Cursor sqlite 分支需要本机有 `sqlite3`。macOS 自带；Linux 上
`apt install sqlite3`。Cursor 写路径在 v0.1 故意拒绝直接修改
`state.vscdb`，v0.2 会加上 "Cursor 必须关闭" 的守卫并提供直接写入。

## <img src="https://api.iconify.design/tabler:topology-star-3.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> 架构

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="./assets/atlas-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="./assets/atlas-light.svg">
    <img src="./assets/atlas-light.svg" width="880" alt="磁盘上被污染的会话 JSONL 由 session 层加载，经启发式建议引擎打分（可选本地 Ollama 重排），在 TUI 中标记，沿 tool_use ↔ tool_result 依赖图做闭包，再带快照保护原子写回">
  </picture>
</p>

磁盘上被污染的**会话文件**（Claude `.jsonl` 或 Cursor `state.vscdb`）先由 **`session`** 层读取，自动识别格式并构建 `tool_use ↔ tool_result` 依赖图。**`suggest`** 流水线用五条纯标准库启发式为每个 turn 打分——只有当你显式加 `--llm` 时，才会把候选短名单交给本地 Ollama 模型重排。候选项在 **TUI 选择器**中预标记，由你确认切除；随后 **`safety`** 给文件做快照（gzip）、原子写回，并保留回滚路径。全程跑在一个静态二进制里——没有守护进程，也不联网。

```
internal/session   loader · claude · cursor · dependency · writer
internal/suggest   启发式（5 个触发器）· scorer · lexicon
internal/llm       可选 Ollama 重排（--llm）
internal/tui       bubbletea 选择器 · diff · model
internal/safety    快照 + edit_log + 回滚
```

## <img src="https://api.iconify.design/tabler:photo.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> 演示

`excise list` → `excise suggest` → 对一个被污染的 Claude Code 会话执行依赖感知的
`excise cut`，由 `docs/demo.tape` 渲染：

![excise 演示 —— 列出被污染的会话、由启发式提名待切 turn，再做带依赖感知拉取的范围切除](assets/demo.gif)

## 路线图

- **v0.2** ✅ —— 启发式建议引擎 + TUI 预标记。
- **v0.3** ✅ —— Cursor `state.vscdb` 读取 + sidecar 切除；可选本地 Ollama LLM 重排。
- **v0.4** ✅ —— 可选**远程**重排后端（OpenAI / Anthropic / OpenRouter），
  共用同一套 `Reranker` 原语（即本版本）。
- **v0.5** —— Codex / Aider / Cline，共用同一套原语。
- **v0.6** —— `excise grep <regex>`，按内容正则批量标记。

## 明确不做的事

- Web UI 或托管服务。只做 CLI/TUI。
- 自动**切**回合。我们只**建议**（v0.2 的 `excise suggest` 是纯启发式，零网络）；
  最终是否切，由你按 enter 决定。`excise autocut` 这条命令永远不会出现。
- **默认**外网调用。LLM 辅助建议一律是**按需开启**的叠加层 —— v0.3 起支持
  本地 Ollama，v0.4 起支持用户自带的远程 API key —— 默认零网络与信任前提
  始终不变。除非你主动开启并提供自己的 endpoint/key，否则什么都不会发到网络。
- Prompt cache 对齐。编辑会让缓存失效；你接受这个代价。
- Claude Code 插件。我们只在两次会话之间动磁盘文件。
- 云同步、账号系统、团队功能。
- 跨工具 session 互通 —— 那是 [cli-continues](https://github.com/cli-continues/cli-continues)
  的事，我们故意不做。
- 任何形式的遥测，**包括 opt-in**。

每一条"拒绝"都是为了让 30 秒的演示视频保持 30 秒。

## 贡献

欢迎 Bug 报告和 PR。提交前请运行 `go test -race ./...`。最有可能改的热点是
`internal/session/claude.go`（schema 兼容性）和 `internal/safety/backup.go`
（快照策略）。更大的改动请先开 Issue，我们对齐一下范围。

## 许可证

[MIT](LICENSE) © 2026 SuperMarioYL
