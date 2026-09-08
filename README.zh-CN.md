[English](./README.md) · [Website](https://excise.lei6393.com) · [GitHub](https://github.com/SuperMarioYL/excise)

<picture>
  <source media="(max-width: 600px) and (prefers-color-scheme: dark)" srcset="./assets/presentation/hero-mobile-dark.svg">
  <source media="(max-width: 600px)" srcset="./assets/presentation/hero-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/hero-dark.svg">
  <img src="./assets/presentation/hero-light.svg" width="960" alt="Hero diagram">
</picture>

# Excise

**带着可见差异移除选定会话轮次。**

Excise 列出并编辑已支持的编码会话，保留稳定轮次 ID 与工具调用关系，写入前展示待删除内容。

## 为什么需要它

失败的支线可能占用上下文，而其余会话仍然有用。可以选定具体轮次、检查依赖警告，并在修改已有支持格式文件时保留快照。

- **保留有用轮次** — 按范围选择，无需丢弃整个会话。
- **写入前检查** — dry-run 展示待应用的会话差异。
- **保留可恢复编辑** — 修改已有目标前生成压缩快照。

## 架构

<picture>
  <source media="(max-width: 600px) and (prefers-color-scheme: dark)" srcset="./assets/presentation/architecture-mobile-dark.svg">
  <source media="(max-width: 600px)" srcset="./assets/presentation/architecture-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/architecture-dark.svg">
  <img src="./assets/presentation/architecture-light.svg" width="960" alt="Architecture diagram">
</picture>

格式加载器生成共享轮次模型；启发式规则推荐候选，可选模型重排进一步排序。依赖图检查选定轮次，TUI 或 cut 命令展示差异，writer 在快照与编辑日志支持下应用修改。

| 组件 | 职责 |
| --- | --- |
| `Transcript loaders` | internal/session |
| `Suggestions + rerank` | internal/suggest; internal/llm |
| `Dependency graph` | internal/session/dependency.go |
| `Diff + write` | internal/tui; session/writer.go |
| `Snapshots` | internal/safety |

## 安装与快速上手

使用仓库清单声明的运行时版本。以下源码安装步骤可复现随仓示例。

```bash
git clone https://github.com/SuperMarioYL/excise.git
cd excise
go build ./cmd/excise
```

Python 3 驱动对随仓会话第 5 轮的 dry-run 删除，并核实 SHA256 未变化。

```bash
python3 examples/presentation_demo.py
```

## 实际运行示例

<picture>
  <source media="(max-width: 600px) and (prefers-color-scheme: dark)" srcset="./assets/presentation/process-mobile-dark.svg">
  <source media="(max-width: 600px)" srcset="./assets/presentation/process-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/process-dark.svg">
  <img src="./assets/presentation/process-light.svg" width="960" alt="Process diagram">
</picture>

The cut preview selects the fifth turn and leaves the input file byte-identical.

```text
Will remove 1 turn(s) (including 0 pulled in by tool-call dependency):
  - u-0003  [user] ~6t  Thanks, that is great.
dry-run; no files written
source unchanged: True
```

完整命令与输出保存在 [docs/demo-results.json](./docs/demo-results.json). 输入和复现代码均随仓提供。

![已有终端录制](./assets/demo.gif)

保留已有录制供参考；上方文字示例给出当前可复现的操作。

## 用法

安装后在仓库根目录运行以下命令；处理自己的数据时替换相应路径。

```bash
go run ./cmd/excise list testdata/claude_session_simple.jsonl
go run ./cmd/excise suggest testdata/claude_session_polluted.jsonl --json
go run ./cmd/excise cut 5 testdata/claude_session_simple.jsonl --dry-run
# Interactive review of your own file:
go run ./cmd/excise pick session.jsonl
```

## 配置

--tool 可选 auto、claude、cursor。cut 使用从 1 开始的 5-7,9 等范围；--dry-run 展示差异，--yes 确认真实写入，--force 跳过依赖警告。快照放在 ~/.excise，后续写快照时清理超过 30 天的旧项。rollback --list 列出 ID，rollback ID --to path 显式指定恢复目标。可选 excise.toml 从项目或 XDG 配置加载，[llm] 支持 backend、model、host、top_n、timeout_sec、provider 与 api_key_env。

## 集成与职责分工

<picture>
  <source media="(max-width: 600px) and (prefers-color-scheme: dark)" srcset="./assets/presentation/integrations-mobile-dark.svg">
  <source media="(max-width: 600px)" srcset="./assets/presentation/integrations-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/integrations-dark.svg">
  <img src="./assets/presentation/integrations-light.svg" width="960" alt="Integrations diagram">
</picture>

根据工作流选择输入与输出路径。本文本地示例验证其中明确说明的子流程。

| 路径 | 已实现职责 |
| --- | --- |
| Claude Code JSONL | Read and rewrite supported records |
| Cursor JSONL / SQLite | SQLite writes an excised sidecar |
| TUI / range CLI | Interactive or explicit selection |
| Ollama / remote API | Opt-in shortlist reranking |
| Gzip snapshots | Rollback for an existing write target |

## 限制与后续方向

- 示例仅 dry-run，并确认源字节不变；不测试运行中的 Agent 重新加载文件。
- Cursor SQLite 编辑生成 .excised.jsonl 旁路文件，不修改在线数据库；首次生成的旁路文件没有旧版本可恢复。
- 模型重排需主动开启，可能向所配端点发送会话片段。启发式建议只是待检查候选，不证明某轮无用。

后续方向是更多会话适配器与端到端兼容性验证；反馈格式不兼容时应提供有代表性的 fixture。

## 许可与贡献

许可见 [LICENSE](./LICENSE). 反馈问题时请提供最小输入、执行命令和实际输出。
