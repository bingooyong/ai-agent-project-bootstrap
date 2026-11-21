# Agent 适配机制证据

访问日期：2026-11-20。范围：WP-00 的指令文件与 Skills 最小语义核实。以下基于直接读取的官方文档，不是对本项目生成物的原生工具运行验收。本次未启动 Codex/Claude/Cursor 来加载夹具，没有安装 Skills、修改全局配置或执行外部文档命令。

## 1. Codex

来源：[Custom instructions with AGENTS.md](https://developers.openai.com/codex/guides/agents-md)、[Skills](https://developers.openai.com/codex/skills)。

| 能力 | 官方资料确认的行为 | OMBP 设计约束 |
| --- | --- | --- |
| 全局指令 | 在 Codex home（默认 ~/.codex，可由 CODEX_HOME 改变）优先 AGENTS.override.md，其次 AGENTS.md；采用首个非空文件 | 不修改用户全局规则；诊断须承认全局上下文可能影响结果 |
| 项目层级 | 从项目根（通常 Git 根）到当前工作目录逐层发现；无项目根时只检查当前目录 | 输出根与目录级规则；不能假定根启动时已加载所有后代规则 |
| 每目录选择 | AGENTS.override.md → AGENTS.md → 配置的 fallback 文件名，每目录至多一个，空文件忽略 | 已有 override 不能被新 AGENTS.md 绕过；冲突需显式报告 |
| 大小预算 | combined size 达到 project_doc_max_bytes 后停止加入，默认 32 KiB | 按实际路径计算累计大小，不只检查各单文件；缺少用户配置时声明使用默认假设 |
| 项目 Skills | 从当前目录向上到仓库根的各层 `.agents/skills` | 项目内 Skill 安装目标须与实际作用域匹配，不默认写全局目录 |
| 用户/其他 Skills | `~/.agents/skills`、`/etc/codex/skills`、工具内置位置 | 存在多个来源，重复名不会合并；不能以同名认为已安全替代 |
| SKILL.md | Skill 为目录，包含必须有 name/description 的 SKILL.md，可带 scripts/references/assets | 安装先检查元数据和内容，存在脚本不代表允许执行 |
| Skill 符号链接 | Codex 支持并跟随 symlinked skill folders | 工具能力不要求 OMBP放宽自身链接安全策略；不默认安装链接 |

官方说明规则按根到当前目录组合，离当前目录更近的指令后出现。这描述工具上下文顺序，不意味着 OMBP 的 P0 保护能由工具自动强制执行。AGENTS.md 与 Skills 都不能代替工具权限和 CI。

## 2. Claude Code

来源：[Memory and instructions](https://code.claude.com/docs/en/memory.md)。本轮官方正文已明确 AGENTS.md 支持的版本与条件；旧讨论中“只有 CLAUDE.md”的假设已不适合作为当前普遍事实。

| 能力 | 官方资料确认的行为 | OMBP 设计约束 |
| --- | --- | --- |
| CLAUDE.md | 当前目录及祖先目录的 CLAUDE.md/CLAUDE.local.md 在启动时组合；后代目录按读取/写入/编辑时按需加载 | 与 Codex 的当前目录发现截点不完全相同，不能只改文件名 |
| 导入 | `@path` 会展开，relative path 相对包含导入的文件；最多四跳；代码块/行内代码中的 @ 不导入 | 适配器可采用明确的 `@AGENTS.md`；普通 Markdown 链接不等于导入 |
| 路径规则 | `.claude/rules/*.md` 使用 Front Matter `paths`，支持 glob；无 paths 为通用规则 | 不复制 Cursor 的 description/globs/alwaysApply 字段；官方说 paths 是该规则读取的字段 |
| AGENTS.md | v2.1.277 起支持直接读取；默认仅在当前目录或祖先没有 CLAUDE.md/CLAUDE.local.md 时读取；Project instructions 设置可改变行为 | 能力需绑定版本及设置；默认生成 CLAUDE.md 导入可减少不同会话行为差异，仍须实验验证 |
| AGENTS 特有差异 | 不读 AGENTS.override.md/AGENTS.local.md 或 `.agents/` 下的指令；直接读取与导入时部分 hooks/外部导入行为不同 | 不能宣称 Codex override 自动适配；必要时生成明确的派生产物与差异提示 |

官方说明旧版本、禁用内置 AGENTS plugin、部分升级后首会话等可能没有直接 AGENTS 支持；`@AGENTS.md` 导入可保留且不会使内容加载两次。外部目录导入会涉及额外批准，不应由生成器替用户批准。为跨平台避免默认创建 symlink，采用普通文件及相对导入更便于审查。

## 3. Cursor

来源：[Rules](https://cursor.com/docs/context/rules)。相较早期资料账本，本轮成功取得可核对正文，以下项可以从“待确认”更新为“官方文档已确认”；原生客户端仍 NOT_RUN。

- Project Rules 放在 `.cursor/rules`，扩展名为 `.mdc`；普通 `.md` 不作为 Project Rule。
- Front Matter 的 `description`、`globs`、`alwaysApply` 控制手动、按路径、Agent判断或总是应用的行为；`alwaysApply: true` 为总是应用，路径规则使用 glob。
- 多个 glob 可用逗号分隔；适配器应测试生成的 Front Matter和作用域，不把其他工具的 YAML `paths` 原样复制。
- 当前官方文档支持根及嵌套 AGENTS.md，后代规则在对应目录及子目录工作时应用，具体规则优先。
- OMBP 仍按规划生成 Cursor 原生 `.mdc`，并对已有 AGENTS.md 的重复或覆盖可能性报告；“工具支持 AGENTS”不证明派生规则和统一源语义等价。

本轮没有核实 Cursor Skills 安装路径、运行版本下限、工具全局配置优先级或总上下文大小限制，这些不能套用 Codex 数值。不得把此段文档确认写成 Cursor Adapter 已通过运行验收。

## 4. 原生验收仍需执行

每个适配器在其交付阶段记录工具版本、配置范围、启动目录、生成文件摘要、观察到的已加载规则和冲突案例。至少覆盖根启动/子目录启动、嵌套细则、已有 override/CLAUDE、超预算、未支持字段、重复 Skills 及多工具同时存在。

Codex 的 32 KiB 默认值不是跨工具统一上限。统一规则引擎保护约束与工具原生优先级分别测试；原生加载成功也不能证明 Agent 每次都遵守自然语言要求。

## 5. 命名轻量核查

本机 `command -v ombp` 未找到命令。2026-11-20 直接查询公开 GitHub API `https://api.github.com/repos/bingooyong/oh-my-blueprint` 返回 `Not Found`。这只表示该未认证公开查询当时未返回可见仓库，不证明名称可创建、没有私有仓库、可注册商标或所有发布命名空间无冲突。

Homebrew、npm scope、域名和商标空间未在本次核查。不得基于此发起仓库更名、注册或发布，现有远程状态仍由实际 Git 配置决定。

## 6. 本轮来源快照摘要

下载的正文只作为资料处理。以下 SHA-256 对应本轮响应字节，方便区分未来文档变化；HTML包含站点导航与动态内容，摘要不能证明官方承诺长期不变，也不是产品安全校验数据。

| 来源 | 字节数 | SHA-256 |
| --- | --- | --- |
| Codex AGENTS HTML | 426677 | `dc5f3401679ece620313815e42f779678ef32649faa52f2cbf6f3d1ea3f68c45` |
| Codex Skills HTML | 436247 | `042b81c5807a13130d0bfd79c3f5baa8a5192e04a43c2e5220a8669e1028799a` |
| Claude Memory Markdown | 55486 | `4186f2be3c7c25188e957096d3e33421afb09467bc0192aa5685a7cc4d8de88f` |
| Cursor Rules HTML | 511041 | `663c71c40a8dcdb6cda2cf186bcb157a1fe9ec195e2a6f23416a25fc9f4f0256` |
