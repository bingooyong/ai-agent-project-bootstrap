# 当前实施与验收状态

日期：2026-11-21。完整目标仍为使用 Trellis 规划并实现 OMBP 的 WP-00～WP-14。离线核心已进入实际命令和平台验收，项目状态仍为 **PARTIAL**。

## 当前可运行范围

- `version`、`scan`、`profile`、`doctor`、`rules list/show/validate`。
- `init` / `diff` 的默认预览、严格 Dry Run、保存计划、计划绑定与显式应用。
- `check` 的当前静态检查、可定位的命令来源和本地指令链接 findings，以及 CI 退出码。
- 根内自定义配置和显式报告写入。计划绑定配置来源与原始字节，Dry Run 拒绝写计划和报告。
- `recover` 的日志恢复、孤立写入锁清理及各自的只读预览。
- P2/P3 自定义规则的显式替代记录，规则验证、计划、锁和 Check 使用同一解析结果。

生成包括项目配置、稳定证据画像、规则锁、根和实际模块的 Codex 指令。读取/写入以 macOS/Linux 的 no-follow 目录句柄实现；Windows 严格 I/O 仍显式拒绝。交叉编译成功不等于 Windows、Linux 或 Agent 原生运行验收。

## 本轮证据

[当前集成结果](../05-testing/evidence/2026-11-21-go-mod-validation-01/results.json)记录 53 个检查与命令场景，全部通过，验证期间源码摘要稳定。本轮还覆盖重复 `go.mod` 版本声明：扫描返回退出码 3，并明确报告 incomplete。`doctor` 会阻断孤立写锁，对事务目录无法检查时也返回 incomplete。Go 1.27.3 与 1.25.5 全树 race 通过。[严格必需字段回归](../05-testing/evidence/2026-11-21-strict-required-fields-01/results.json)补充验证递归缺项拒绝、可选字段接受及已存画像完整覆盖要求。[Go 模块语法回归](../05-testing/evidence/2026-11-21-gomod-directive-syntax-01/results.json)覆盖有效预发布版本/工具链语法、非法版本和工具链语法以及块内顶层声明。[Go require 版本回归](../05-testing/evidence/2026-11-21-gomod-require-version-01/results.json)覆盖 inline/block 依赖版本语法和不完整 semver token。较早的 [WP-06 记录](../05-testing/evidence/2026-11-21-wp06-integration-01/results.json)和 [规则/恢复记录](../05-testing/evidence/2026-11-21-provenance-recovery-02/results.json)保留历史状态。

| 平台 | 实际验证 | 当前边界 |
| --- | --- | --- |
| macOS arm64 | Go 1.27.3 和 1.25.5 全树 race，原生 CLI 命令 | 覆盖 Go 1.25 系列的已安装补丁版 |
| macOS amd64 | 交叉构建 | 原生运行未测 |
| Linux amd64 | 交叉构建 | 原生运行未测 |
| Linux arm64 | [隔离容器原生运行](../05-testing/evidence/2026-11-21-go-mod-validation-linux-02/results.json)：14 个 CLI 场景、4 个包测试，包括重复版本声明的 incomplete 和不安全事务目录的 incomplete 报告 | 容器中禁网，未启用 race 或 CGO |
| Windows amd64/arm64 | 交叉构建 | 严格项目 I/O 尚未实现 |

[扫描支持矩阵](../05-testing/scanner-support.md)覆盖 6 个合成工程，核对模块、命令和声明来源。[CLI 资源测量](../05-testing/evidence/2026-11-21-scanner-budget-02/results.json)验证了 20,000 条目以内完整扫描、超限返回 3/incomplete、排除后恢复。测量针对本地合成工程，不估算真实仓库识别精度。

[Codex 原生加载证据](../05-testing/evidence/2026-11-21-codex-native-02/results.json)在 macOS arm64 使用 Codex CLI 0.169.0，禁网运行 `debug prompt-input`。四种场景验证了实际生成的根/子目录指令、手写祖先加载顺序、兄弟目录隔离和 override 优先级。范围限于工具构建上下文，没有执行模型请求或验证模型遵循指令。

真实子进程中断回归覆盖从获得写入锁到清理提交日志的七个窗口；子进程使用 `os.Exit(91)` 绕过 defer。无日志的两个窗口会留下只可显式清理的孤立锁，清理前重查摘要、原进程和日志缺失；`doctor` 会报告写锁，恢复预览经确认后只移除该锁。错误项目的日志在写入前拒绝。文件操作仍依赖合作式协议；任意并发目录替换、硬件断电和不同进程命名空间的恢复不属于本轮证据。

保留早期核心集成的 [01 轮记录](../05-testing/evidence/2026-11-21-core-integration-01/results.json)、[02 轮失败](../05-testing/evidence/2026-11-21-core-integration-02/results.json)和[修复后的 03 轮](../05-testing/evidence/2026-11-21-core-integration-03/results.json)。01 早于独立审查修复；02 因格式检查停止。Codex 原生加载的首轮环境失败也[单独保留](../05-testing/evidence/2026-11-21-codex-native-01/results.json)。

## 独立审查与修复

只读 Trellis 检查代理复现了三项问题，主线已修复并增加回归，代理再次复核通过：

1. **可编辑锁隐藏必需规则缺失。** Check 现在验证解析后的规则清单、必需输出集合，并在来源快照未变时对比真实重新渲染的受管正文；Build 同样拒绝伪造清单/正文。匹配可编辑哈希不再是唯一依据。
2. **漏算未受管的中间祖先指令。** Check 读取真实祖先 AGENTS/override 链并校验累计预算；Build 将祖先摘要及 override 的缺失状态绑定到计划。
3. **外部计划路径漏查敏感父目录。** 保存/读取计划改为完整路径敏感策略与逐组件 no-follow，拒绝用户创建的父目录链接；macOS 的固定系统别名单独正规化。

独立复核验证了相关 race 测试、vet 与编译；最后发现的格式问题已修复并由 03 轮重验。该审查是有界代码审查，不是完整安全认证。

## MVP 逐项状态

| 验收项 | 当前证据与缺口 | 状态 |
| --- | --- | --- |
| AC-01 证据化项目识别 | 六组合成夹具验证语言、模块、命令及证据；Node 包管理器字段、锁文件和工作区继承可溯源；Go/TOML 仍为静态子集 | PARTIAL |
| AC-02 规则解析与 Codex 输出 | 全局/模块渲染及预算有回归测试，macOS 上四种 Codex 启动方式已实测 | 当前切片通过 |
| AC-03 预览/显式计划应用 | CLI 进程验证 Dry Run 无项目写入、根目录绑定、计划重算、应用及重复生成 | 当前切片通过 |
| AC-04 人工/已有文件保护 | 未接管、受管区修改、override、链接、过期来源等被拒绝；完整接管/迁移尚未实现且不自动覆盖 | PARTIAL |
| AC-05 锁与可复现 | 已锁工具/规则/适配器/模板版本及摘要、配置与来源指纹；Schema 迁移和升级协议仍需交付 | PARTIAL |
| AC-06 静态 Check | 已验证当前规则、命令来源、受管输出、祖先预算及本地指令链接；引用覆盖单行内联 Markdown，外部资源与完整 Markdown 语法不在当前范围 | 当前切片通过 |
| AC-07 自定义规则与保护 | 精确依赖、冲突、P0/P1 保护及单步 P2/P3 替代已有正反例与 CLI 验证；签发、跨作用域替代与已锁版本升级保留后续设计 | 当前切片通过 |
| AC-08 安全夹具与恢复 | macOS 和 Linux arm64 已通过链接、计划、归属与进程中断回归；其他平台及更多故障面仍待验证 | PARTIAL |
| AC-09 平台与追溯 | macOS 两个 Go 系列的原生测试及平台交叉构建有证据，仍未覆盖目标架构的完整原生矩阵 | PARTIAL |
| AC-10 不混报后续能力 | 后续多 Agent、Skills、AI、企业治理仍列为 PLANNED，没有成功空实现 | 当前报告满足 |

## 接续任务

1. 完成 WP-01/02 的剩余 Schema 语义与 Go/TOML 支持边界审查。
2. 按 [Windows 文件访问的设计和验收条件](../../.trellis/tasks/11-20-ombp-wp07/research/windows-strict-io.md)实现严格 I/O；本机未找到原生环境，当前仍明确拒绝。事务目录的长期保留和清理策略仍待完成。
3. MVP 全项审查后按 DAG 推进版本升级与迁移、Update/Audit、多 Agent、Skills、Registry、AI 与团队治理。

当前分支为 `feat/ombp-offline-core`，本轮增量尚未提交。没有执行远程更名、发布制品、部署、生产连接或用户全局配置修改。
