# CLI 详细设计

版本：V0.2；状态：产品命令协议，正在分阶段实现。产品为 Oh My Blueprint（OMBP），可执行文件与 Go 二进制统一为 `ombp`。当前可用命令以 CLI help、根 README 和实际运行证据为准；后续阶段命令不会预先提供成功占位。

## 1. 命令分期（D-CLI）

| 阶段 | 命令 | 行为 |
| --- | --- | --- |
| MVP | `version`、`doctor [path]` | 版本及工程配置健康诊断：路径权限、已有 Agent 配置、规则/lock 兼容性、未完成事务；不安装、不联网、不执行项目命令 |
| MVP | `scan [path]`、`profile [path]` | 静态扫描和画像；profile 默认读取已存画像，无画像时报配置问题 |
| MVP | `init [path]` | 扫描、解析、预览；显式 apply 才写项目 |
| MVP | `check [path]` | 只读静态检查、输出 findings |
| MVP | `rules list/show/validate` | 查看内置/项目规则及结构化验证，不改规则源 |
| MVP | `diff [path]` | 生成候选与当前文件差异；不修改文件，不自动应用 |
| Phase 2 | `update [path]`、`audit [path]`、`rules audit/update [path]` | 漂移、版本更新计划及更深静态审计；rules 子命令限定规则范围 |
| Phase 3 | `agents list/enable/sync`、`sync [path]` | Agent 能力与派生配置；sync 默认为 Agent 配置同步，enable/sync 变更也走计划 |
| Phase 4 | `skills list/search/show/install/update/uninstall/audit/sync` | 推荐、检查和显式安装；Skills sync 对齐已锁定清单，不自动升级；联网与写入分别授权 |
| 后续建议能力 | `evolve [path]` | 根据工程漂移提出带证据的演进建议；非 MVP，不直接修改规则或业务代码 |
| 后续 | `stack list/show/add`、`rules add/remove` | 技术栈和规则配置变更，受保护规则不可移除 |
| 后续独立设计 | `probe`、恢复/迁移命令 | 具体命令执行授权、事务恢复和已有文件接管 |

讨论中的 `analyze`、`add-stack`、`split-rules` 不另设同义命令，分别归入 scan/audit、stack add 和 init/update 的作用域规划。`install` 不作为“安装工具自身”的子命令；分发渠道另行设计，不发布尚不存在的 Homebrew/npm 安装指令。

### 命令风格与范围

统一使用 `ombp <verb>` 或 `ombp <resource> <verb>`。面向用户的资源名采用 `agents`，内部实现仍称 Adapter；早期设计中的 `adapters` 命令在发布前替换，无需保留尚未发布的兼容别名。

`ombp init` 本来就包含静态扫描；`--scan` 为显式扫描意图标记，行为与默认一致，不触发命令执行。`--agent codex` 选择目标适配器，MVP 只接受 codex，其他值明确报不支持。初始化默认 preview，以上参数均不授权写入。

`rules audit/update` 复用顶层 audit/update 的规则子集，不更新 Skills 或其他资源。`agents sync` 与顶层 `sync` 使用同一 Agent 配置同步流程；`skills sync` 独立对齐项目锁定清单。各命令只在相应阶段开放，所有写入都遵循 preview/plan/apply；网络下载须另有明确授权。

`evolve` 在后续阶段输出演进建议与候选 Diff，不能成为绕过 update 计划校验的写入入口。它与 audit 的区别是提出可审阅的改进方向，不能把建议说成已确认的项目事实。

## 2. 通用参数与 I/O

| 参数 | 约定 |
| --- | --- |
| `[path]` | 缺省当前目录；规范化为目标根；不默认向上寻找别的工程根，不接受远程 URL |
| `--format text\|json` | 缺省 text；JSON stdout 只输出一个完整对象，诊断在 stderr |
| `--ci` | 禁止交互，启用确定的阻断策略；与 apply 互斥；不暗含网络或写入 |
| `--output <file>` | 在目标根内已有目录中创建报告，格式随 `--format`。拒绝覆盖和链接，并保护敏感路径、工程声明及治理文件 |
| `--config <relative-file>` | `scan`、`rules validate`、`doctor`、`check`、`init`、`diff` 可选择已有的根内配置。配置缺失、格式错误、越界或链接均返回 2 |
| `--exclude <pattern>` | 增加静态扫描排除，不得取消敏感文件拒绝策略；报告列出排除范围 |
| `--deep` | 仅 scan 的扩展静态探测；同样遵守读取预算和隐私边界 |

CLI 不将仓库文本当作 Shell 参数执行。参数错误不写目标工程。text 模式为人阅读，集成方依赖版本化 JSON 和退出码，不能解析自然语言提示。

`--output` 是对报告文件的明确本地写入授权，不是应用项目规则的授权；输出已存在时默认拒绝覆盖。严格 Dry Run 不允许持久化计划/报告参数，保证零文件写入。

保存预览或恢复报告时，文件中只保留相对路径、摘要等审阅信息。完整改动仍可在 stdout 审阅，`--plan-out` 负责保存可应用的完整计划。自定义配置的路径和文件摘要绑定到计划，回放使用已审阅的配置。应用后如锁引用了自定义配置，`check`、`doctor`、`init`、`diff` 需要显式选定配置，锁中的路径不会触发隐式读取。

## 3. 预览、计划与应用

`init`、后续 `update/sync` 缺省 preview。preview 可以显式保存计划，但不写工程规则；`--dry-run` 则禁止任何持久化输出。所有可写命令沿用同一行为。

```sh
# 未来产品命令示例：只预览，不写文件
ombp scan . --format json
ombp init . --dry-run --print-diff
ombp init --scan --agent codex --dry-run

# 预览并显式保存计划，所选路径不得已存在
ombp init . --print-diff --plan-out /tmp/ombp-plan.json

# 明确应用先前审阅的计划；重新扫描和核验，不执行其中的任意指令
ombp init . --apply --plan /tmp/ombp-plan.json

# 静态检查；不能证明工程构建、测试或 Agent 实际运行成功
ombp check . --ci --format json
```

| 参数 | 约束 |
| --- | --- |
| `--dry-run` | 强制只读；与 apply、plan-out、output 互斥 |
| `--print-diff` | 在 text stdout 展示 unified diff；JSON 中使用 `changes[].diff` |
| `--plan-out <file>` | 只在 preview 可用，明确保存计划；允许目标工程外用户选定位置，但不覆盖、不跟随链接；不改变规则文件 |
| `--apply` | 必须配 `--plan`；不以默认回车或宽泛 `--yes` 代替计划范围校验 |
| `--plan <file>` | 只能读取有界、非敏感、非链接计划；验证 Schema、根、输入摘要、目标清单和重新渲染结果 |

preview 中出现阻断冲突时不输出可应用计划。apply 遇到任何输入变化返回计划失效，生成新预览的建议由用户或 Agent 在已有任务授权范围内执行。apply 只授权计划中的本地配置，不授权联网、脚本执行、Git、生产操作或全局文件写入。

`recover` 默认只读。存在事务日志时先检查其内容和项目归属，应用用 `--apply --journal-sha <sha256>`。无日志且能确认原写入进程已退出时，预览返回 `orphaned_mutex`，用 `--apply --mutex-sha <sha256>` 仅移除审阅过的孤立锁。两种摘要不得混用，`--ci` 禁止应用。

`--plan-out` 可写用户明确指定的目标根外文件，是规则输出“根内写入”策略的单一可审计例外；同样拒绝敏感路径和已有文件。计划包含目标本机规范路径，不建议提交 Git。

## 4. 退出码与阻断策略

| 退出码 | 含义 | 示例 |
| --- | --- | --- |
| 0 | 该命令在声明的检查范围内成功 | 画像完成；静态检查无达到阈值问题；无变化的有效计划 |
| 1 | 非阻断级质量问题达到阈值 | 目录导航过时、可修复冗余或必需信息待确认 |
| 2 | 参数、配置、版本协议错误 | 无效参数、无效 Schema、不兼容主版本、非法路径参数 |
| 3 | 操作未完整执行 | 文件读取失败、扫描预算触限、写入失败、恢复未完成；输出 `operation_status=incomplete` |
| 4 | 安全/完整性或应用阻断 | 受管内容被改、过期计划、路径链接、受保护规则冲突、缺失锁定内容 |

解析参数失败优先返回 2；其余先判断是否发生不完整操作（3），完整执行后有安全/完整性阻断返回 4，再判断质量阈值（1），否则 0。单个报告仍包含所有已收集 findings，不能因最终选用一个退出码而丢弃其他问题。

`check/audit` 的默认质量阈值为 Medium 及以上；Low/Informational 不改变 0。安全和完整性阻断与等级阈值分别计算，不能靠设置阈值隐藏。后续可配置 `--fail-on`，MVP 不提供忽略阻断项的开关。

0 仅表示本条命令在已声明范围成功。静态 Check 的 0 不代表构建通过、运行安全或企业 UAT 通过。scan 的 unknown 是画像状态；check 遇到必须确认但未知的关键信息按质量问题报告。

## 5. JSON 报告协议（D-REPORT）

实际 CLI 通用报告结构由 [cli-report.schema.json](../schemas/cli-report.schema.json) 描述。`data` 随命令和输出方式变化，报告外层的退出码、发现和状态有固定协议。

```json
{
  "schema_version": "1.0",
  "command": "check",
  "mode": "static",
  "operation_status": "complete",
  "result": "issues_found",
  "exit_code": 1,
  "tool_version": "<release-version>",
  "coverage": {"status": "complete", "excluded": ["node_modules"]},
  "findings": [
    {
      "code": "COMMAND_SOURCE_REMOVED",
      "severity": "Medium",
      "blocking": false,
      "rule_ids": ["commands.source-traceable"],
      "location": {"path": "package.json", "pointer": "/scripts/test"},
      "evidence": "已记录的脚本键在当前清单中不存在",
      "impact": "Agent 可能调用过时测试命令",
      "recommendation": "核对包管理器与当前脚本，生成更新计划",
      "requires_confirmation": true,
      "method": "static"
    }
  ],
  "command_execution": "NOT_RUN",
  "changes": []
}
```

`result` 使用 `clean/issues_found/blocked/error`；operation_status 使用 `complete/incomplete`。候选置信度不能替代这两个维度。命令探测的未来报告需另含 argv、cwd、环境标识、开始/结束时间、退出码和脱敏日志；没有这些不得写 EXECUTED 或 PASS。

位置优先相对路径 + JSON pointer/配置键/受管区 ID；有可靠行号时增加 line。不在报告中保存源码全段、凭证值和绝对用户路径。findings 先按阻断、等级、路径、定位和 code 排序，便于稳定比较。

命令来源诊断区分声明缺失、声明或调用方式发生变化、来源无法核实。文档检查覆盖已读取的指令文件中的单行 Markdown 内联文件链接，仅查询目标元数据。外部链接、锚点和代码示例不在检查范围内。链接缺失返回质量问题，链接越界或指向符号链接返回阻断，元数据读取失败或触及链接检查预算返回未完整执行。

## 6. 关键错误处理

| 场景 | 产品行为 |
| --- | --- |
| 空目录 | 输出未知技术栈；只建议通用规则，不生成假构建命令 |
| 无效项目清单 | 保留已知事实，标 incomplete，scan 返回 3，init 禁止 apply |
| 已有手写 AGENTS.md | 输出待迁移冲突，不覆盖、不插入伪受管标记 |
| 源清单在 preview 后变化 | apply 返回 4；不进行部分写入 |
| 其他编辑器在事务中改文件 | 摘要检查失败；恢复仅处理仍属于本事务的内容 |
| 写入中断 | 保留 journal 与备份；后续 apply 阻断，提供恢复状态 |
| CI 无网络 | 内置包仍可 check；锁定外部包缺失时明确阻断，不自动下载 |
| 无支持的适配器能力 | 报能力缺口，不静默降级为简单改文件名 |

## 7. 测试设计

领域测试覆盖探测证据、画像修正、依赖闭包、冲突、版本和 canonical hash。CLI 表驱动测试覆盖参数互斥、stdin/tty/CI、退出码和 stdout/stderr。

完整流程测试用临时工程执行 preview、plan、apply、check，再次生成无差异；在每个文件替换点注入中断验证恢复。Golden 固定版本与输入，在不同路径/遍历次序下比较正文和摘要。恶意输入夹具覆盖敏感文件、越界路径、符号链接、大小写别名、标记损坏、巨大 YAML 和伪造计划。

测试不使用真实生产库、不依赖用户全局 Skills、不上传源码。运行层验证和产品静态结果分开存档。详细追溯见[验证矩阵](06-Validation.md)。
