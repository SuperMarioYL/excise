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

## 命令一览

```
excise [path]                      打开 TUI（无参数则自动发现最新会话）
excise list [path]                 打印回合表，不修改
excise pick [path]                 显式打开 TUI（与裸命令同义）
excise cut <range> [path]          非交互式切除；如 "5-7,9"
excise rollback --list             列出所有快照，最新在前
excise rollback <snapshot-id>      恢复某个快照

全局参数：
  --tool       auto|claude|cursor   会话格式（默认 auto）
  --session    PATH                 显式指定会话路径
  --force                           即使有依赖警告也继续
  --dry-run                         只显示差异，不写文件
  -y, --yes                         跳过最终确认
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

## 架构

```
┌────────────────────────────┐
│  cmd/excise/main.go        │  cobra CLI: excise [pick|list|cut|rollback]
└────────────┬───────────────┘
             │
   ┌─────────┴────────────────────────────┐
   ▼                                      ▼
internal/session                     internal/tui
  loader.go      Tool / Turn 模型       model.go   纯状态机 + token 计算
  claude.go      Claude JSONL          picker.go  手写 stdin 驱动
  cursor.go      Cursor sqlite+jsonl   bubbletea.go  真正的终端 UI
  dependency.go  工具调用图             diff.go    前后对比摘要
  writer.go      WriterFor() 分派
                    │
                    ▼
            internal/safety
              backup.go    快照 + edit_log + 回滚
```

一个二进制，没有守护进程，没有 IPC。

## 路线图

- **v0.2** — 直接写 `state.vscdb` + "Cursor 已关闭吗" 守卫；多会话窗口支持。
- **v0.3** — Codex / Aider / Cline，共用同一套原语。
- **v0.4** — `excise grep <regex>`，按内容正则批量标记。
- **v0.5** — 一个 session 调试器 sidecar，让你只查看工具调用图、不做任何切除。

## 明确不做的事

- Web UI 或托管服务。只做 CLI/TUI。
- 自动识别"被污染"的回合。**你来选**，我们不调用 LLM。
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
