# 文档导航

本文档集记录 **Oh My Blueprint（OMBP）** 的产品背景、需求、设计与实施规划。

**Agent Engineering Blueprint · One Blueprint. Every Agent.**

一套工程蓝图，统一驱动所有 AI Agent。文档基线为 **V0.2，2026-11-20，设计草案**。本轮交付限于文档，尚无 CLI、规则包、Schema 或 Skills 实现，也未形成产品运行验收结论。

| 文档 | 内容 | 建议读者 |
| --- | --- | --- |
| [00-Background.md](00-Background.md) | 建设背景、治理原则、讨论归纳 | 所有参与者 |
| [01-PRD.md](01-PRD.md) | 用户场景、需求编号、MVP 边界、验收条件 | 产品、开发、测试 |
| [02-Architecture.md](02-Architecture.md) | 模块边界、数据流、安全边界、技术决策 | 架构、开发 |
| [03-Rule-Engine-Design.md](03-Rule-Engine-Design.md) | 规则模型、解析算法、冲突、锁定、更新事务 | 规则维护者、开发 |
| [04-CLI-SDD.md](04-CLI-SDD.md) | 命令协议、参数、退出码、文件操作、输出 | 开发、测试、集成方 |
| [05-Roadmap.md](05-Roadmap.md) | 阶段范围、工作包、依赖、门禁、风险 | 项目负责人、开发 |
| [06-Validation.md](06-Validation.md) | 验证矩阵、证据规范、本轮文档检查记录 | 测试、审查者 |
| [07-References.md](07-References.md) | 外部资料、核实状态、待完成调研 | 适配器维护者、审查者 |
| [08-Naming.md](08-Naming.md) | 品牌、命名空间、命令风格与更名边界 | 所有参与者 |

## 阅读与维护约定

产品品牌统一为 **Oh My Blueprint**，目标仓库名为 `oh-my-blueprint`，CLI 与 Go 二进制为 `ombp`，项目配置目录为 `.ombp/`。完整约定见[命名规范](08-Naming.md)。这些名称已用于文档设计，命名空间可用性尚未完成核实。

本轮读取到的实际 Git 远程仍为 `https://github.com/bingooyong/ai-agent-project-bootstrap.git`。远程更名、本地目录迁移和制品注册尚未执行；当前文档中的新仓库名与 Go Module 路径表示目标命名。

需求以 PRD 的 `FR-*`、`NFR-*`、`AC-*` 为索引，设计以 `D-*` 为索引，规划以 `WP-*` 为索引。需求变化时同步检查设计、工作包和验证矩阵。代码位置和测试证据只有实现后才能填写，不预先写成已存在路径或已通过测试。

本文中的命令、配置和目录树均为**拟议产品协议或目标结构**，不代表当前可执行。设计选择可用于后续评审；依赖、外部工具能力和适配器行为应在实施前按资料账本复核。

## 已收敛的范围

- 首期以离线 Go CLI、Codex 适配、项目画像、规则生成、文件保护、锁定和静态检查为交付范围。
- MVP 即包含基础锁文件、受管内容校验与必要的冲突阻断；Phase 2 再扩展漂移、审计和更新能力。
- Claude Code、Cursor 属于产品目标，安排在 Phase 3；Gemini 后续按验证情况扩展。
- Skills 是产品目标，Phase 4 交付来源审查、项目内安装与版本管理；MVP 不自动安装第三方 Skills。
- `scan --deep` 仍只进行静态读取；执行构建命令须使用后续独立探测模式，不由扫描触发。
- CI、分支保护、工具权限属于强制控制；AGENTS.md 和 Skills 提供行为指导，不能替代这些控制。
