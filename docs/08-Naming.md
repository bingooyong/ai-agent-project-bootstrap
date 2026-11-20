# 品牌与命名规范

版本：V0.2；日期：2026-11-20；状态：文档采用的命名决策，外部分发空间待核实。

## 1. 品牌定位（D-NAME）

| 项目 | 统一名称 |
| --- | --- |
| 品牌 | **Oh My Blueprint** |
| 简称 | **OMBP** |
| 定位 | **Agent Engineering Blueprint** |
| 英文标语 | **One Blueprint. Every Agent.** |
| 中文说明 | 一套工程蓝图，统一驱动所有 AI Agent |
| 能力范围 | 项目初始化、工程规则、Skills、多 Agent 适配与持续治理 |

Blueprint 包括项目画像与结构、规则与安全基线、Skills 工作流、Agent 配置以及版本/冲突/漂移治理。命名不绑定单个编码工具，也不限制为 AGENTS.md 生成器；“Every Agent”表达长期方向，实际兼容性以适配器支持矩阵和验证结果为准。

## 2. 技术命名空间

| 对象 | 目标名称 | 当前状态/约束 |
| --- | --- | --- |
| GitHub 仓库 | `bingooyong/oh-my-blueprint` | 目标名称；尚未执行远程更名 |
| Go Module | `github.com/bingooyong/oh-my-blueprint` | 目标路径；尚无 go.mod，需先核实仓库路径 |
| CLI / Go 二进制 | `ombp` | 统一命令名；尚无制品 |
| CLI 入口 | `cmd/ombp/` | 目标源码目录 |
| 项目配置 | `.ombp/` | `project.yaml`、`profile.json`、`lock.yaml`、rules/reports/state 采用原设计职责 |
| 全局配置 | `~/.ombp/` | 保留命名空间；MVP 不创建或加载，启用前另行设计权限及优先级 |
| 环境变量 | `OMBP_` | 保留前缀；具体键逐项设计，不自动导入所有同前缀变量 |
| 自有 Skills | `ombp-` 前缀 | 如 `ombp-project-init`、`ombp-code-review`、`ombp-project-acceptance`；非已发布包 |
| 自有规则包 | `ombp-` 前缀 | 如 `ombp-core`、`ombp-security`、`ombp-golang`；不强改稳定规则 ID |
| 受管区块 | `ombp:begin` / `ombp:end` | 用于新设计产物；旧标记不可不经审核自动接管 |
| Homebrew Formula | `ombp` | 候选；未核实、未注册、未发布 |
| npm 包 | `@oh-my-blueprint/cli` | 可选候选，首期无发布承诺；scope/包名未核实 |
| 域名 | `ohmyblueprint.dev` 等 | 候选，未核实可注册性或商标关系 |

用户全局配置空间与 Agent 原生指令/Skills 路径分别建模；`.ombp/` 不替代 Codex 的 AGENTS.md 或各工具规定的 Skills 搜索路径。第三方 Skill/规则包保留其原 ID 和来源，不机械更名为 `ombp-*`。

## 3. 命令风格

初始化使用 `ombp init`，项目识别使用 scan/profile，配置诊断使用 doctor，治理使用 check/audit/diff/update。面向用户的工具资源名为 `agents`，内部模块仍使用 adapters。

`ombp sync` 与 `ombp agents sync` 同步已选 Agent 配置；`ombp skills sync` 在 Phase 4 对齐锁定 Skills；`ombp evolve` 留作后续建议能力，不自动应用。规则 audit/update 子命令与顶层对应命令共享实现、限定范围，禁止出现不同安全语义。参数、分期和写入规则以[CLI SDD](04-CLI-SDD.md)为准。

自有 Skill 的未来调用示例为 `ombp skills install ombp-code-review`，不代表当前可执行或该 Skill 已发布。`init --scan` 不扩展扫描权限，`init --agent codex` 不绕过 apply。

## 4. 命名选择与历史

V0.1 使用描述性 CLI 名和旧配置目录。本轮文档统一更名，保留原需求编号、阶段划分和安全模型。旧名称只在历史记录与实际远程状态说明中出现。

用户排除 `oh-my-agent`、`agentry`、`agent-seed`、`agent-loom`、`agent-forge` 等名称，并保留 Oh My DevKit（`omdk`）和 Rulewright（`rwl`）为备选。排除原因来自用户提供的检索摘要，本轮未独立核实这些项目的当前状态；备选不进入代码、包名或配置。

本轮开始时实际远程为 `https://github.com/bingooyong/ai-agent-project-bootstrap.git`。本地工作目录也仍使用旧仓库目录名。文档更名不等于 GitHub 更名、重定向验证、本地迁移、Go Module 发布或注册商标。

## 5. 实施与核查顺序

1. 分别核实 GitHub 目标仓库、当前机器 `ombp` 命令、Go 路径、Homebrew、可选 npm scope、域名和商标空间；记录查询时间、来源、结果及限制。“没有搜到”不能证明名称唯一。
2. 需要实际远程更名时，单独执行仓库更名并验证新 URL、权限和旧 URL 跳转，再更新 origin、README 链接和 Module 路径；本轮未执行这些外部变更。
3. 工程初始化统一使用 `cmd/ombp` 与目标 Module，自有规则包和 Skills 遵循前缀；所有制品名称与帮助示例检查一致性。
4. 若已有旧目录或标记，提供明确迁移计划、差异、冲突和备份策略；不自动移动用户配置或改写 lock。当前尚无已发布版本，不承诺旧 CLI 的兼容别名。
5. 后续 Codex/Trellis 任务书采用本规范；不得以命名确认代替产品实现或发布验收。
