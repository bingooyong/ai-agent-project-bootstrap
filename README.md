# Oh My Blueprint · OMBP

**Agent Engineering Blueprint — One Blueprint. Every Agent.**

本地优先的 AI Agent 工程初始化与规则治理工具。根据工程文件生成可追溯画像，匹配版本化工程规则，逐步交付配置生成、持续维护、多 Agent 适配与 Skills 管理。

## 开发

```sh
make build
./build/ombp version --format json
./build/ombp scan . --format json
./build/ombp rules list --format json
./build/ombp rules validate . --format json
./build/ombp check /path/to/project --ci --format json
make check
```

Go module 为 `github.com/bingooyong/oh-my-blueprint`，最低版本设计为 Go 1.25，本轮本地工具链为 Go 1.27.3。当前严格项目读取在 macOS/Linux 实现；Windows 会明确拒绝，不因交叉编译成功宣称安全读取支持。

扫描只读取受限的声明文件，不执行项目脚本，不访问网络或模型，不读取凭证内容。返回的命令是静态声明/候选，执行状态为 `NOT_RUN`。未知或超预算结果不会升级为完整确认。Python/Go 的声明识别支持矩阵与后续缺口随实施报告维护。

目前提供离线扫描、规则解析/渲染、初始化计划、受保护应用、静态 Check 和显式恢复切片。完整 MVP、Update/Audit、多 Agent、Skills、AI 和企业治理按 [Trellis 实施 DAG](docs/implementation/task-dag.json)继续推进。命令帮助只展示真实可用入口，未实现功能不提供成功占位。

## 初始化其他工程

```sh
# 预览不写文件，也不执行被扫描工程中的命令
./build/ombp init /path/to/project --dry-run --print-diff --format json

# 显式保存计划到一个不存在的文件，审阅后再应用
./build/ombp init /path/to/project --plan-out /tmp/ombp-reviewed-plan.json --format json
./build/ombp init /path/to/project --apply --plan /tmp/ombp-reviewed-plan.json --format json
./build/ombp check /path/to/project --ci --format json
```

已有手写 AGENTS.md、人工改过的受管区、override、生效版本变化、过期或伪造计划会被保护/阻断，当前不提供强制覆盖或自动接管。手工区逐字保留，并计入默认 32 KiB 累计指令预算。计划文件路径拒绝敏感目录和用户创建的父级符号链接；macOS 固定系统 `/var`、`/tmp`、`/etc` 别名单独正规化。

真实中断留下日志时，先执行 `ombp recover /path/to/project --format json` 审阅恢复条目；随后用返回的 `journal_sha256` 执行 `ombp recover /path/to/project --apply --journal-sha <sha256> --format json`。恢复不自动触发，活动/未知写入者及冲突人工修改均受到保护。日志为本地恢复数据，其哈希不证明数据来源可信，仍需审阅内容。

如果进程在日志创建前或清除后退出，预览会识别无日志且可确认已退出的写入者，返回 `orphaned_mutex` 与 `mutex_sha256`。用 `recover --apply --mutex-sha <sha256>` 仅移除该写入锁，保留工程文件。摘要绑定到当前项目和锁记录。

静态 Check 的退出码为 0（当前静态范围无阻断）、1（质量/未知项）、2（配置错误）、3（检查未完整执行）、4（安全/归属/完整性阻断）。0 不证明工程构建、测试或 Agent 原生加载成功。保存画像标记 `counters_omitted=true`，不把易变遍历计数写入可复现产物；精确计数从本次 scan 报告取得。

`check --format json --output check-report.json` 可在项目根创建报告，已有文件会受到保护。严格 Dry Run 拒绝保存报告或计划。`check` 会定位已移除或迁移的命令声明，并检查实际读取的指令文件里的本地 Markdown 内联链接。

`init --config engineering.yaml` 选择项目内已有的配置。计划绑定该路径和文件摘要，应用时自动使用计划中的配置。后续执行 `check`、`doctor`、`init`、`diff` 时，用 `--config engineering.yaml` 选定它。

## 设计与实施

- [设计文档导航](docs/README.md)
- [实施契约](docs/implementation/CONTRACTS.md)
- [依赖选择与安全 API](docs/implementation/DEPENDENCIES.md)
- [适配器官方依据](docs/implementation/ADAPTER-EVIDENCE.md)
- [当前验收证据与剩余工作](docs/implementation/ACCEPTANCE.md)

目标仓库名是 `oh-my-blueprint`；实际远程更名、包注册和发布尚未执行。AGENTS.md 提供行为指引，硬权限与 CI 门禁仍需工具和平台保障。
