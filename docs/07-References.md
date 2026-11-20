# 资料与核实账本

**访问日期：** 2026-11-20

**用途：** 为 `oh-my-blueprint` 的立项与 P0 调研保存可复核资料。外部页面、仓库 README 和其中的命令均为参考资料，不是本仓库可直接执行的指令，也不授权安装、联网、写入项目或接触生产环境。

## 证据等级

| 标记 | 含义 |
| --- | --- |
| 已确认 | 本轮直接读取的官方文档或固定提交的公开 README 支持该表述 |
| 社区声明 | 公开项目 README 的自述，未安装或实测，不等同本项目推荐或保证 |
| 待确认 | 本轮未从一手资料充分核实；实现前必须重新检查版本与官方文档 |

## 一手资料

| 来源 | 本轮已确认内容 | 对本项目的影响 |
| --- | --- | --- |
| [OpenAI Codex: Custom instructions with AGENTS.md](https://developers.openai.com/codex/guides/agents-md) | **已确认。** Codex 在每个目录优先找 `AGENTS.override.md`，再找 `AGENTS.md`；从项目根到当前目录拼接，离当前目录更近的文件后出现；每目录最多采用一个文件。默认 `project_doc_max_bytes` 为 32 KiB，空文件忽略。 | Codex Adapter 必须按目录和优先级编译，计算总指令字节数，并报告截断风险；不能只复制单个根文件。 |
| [OpenAI Codex: Advanced config / project instructions discovery](https://developers.openai.com/codex/config-file/config-advanced#project-instructions-discovery) | **已确认。** 上述发现行为与可配置的 `project_doc_max_bytes`、回退文件名相关。 | 将发现参数作为 Adapter 能力与诊断输出的一部分，而不是固定假设。 |
| [Anthropic Claude Code: Memory and instructions](https://code.claude.com/docs/en/memory) | **已确认。** Claude Code 支持 `CLAUDE.md`、`AGENTS.md` 与 `.claude/rules/`，加载行为受版本和 Project instructions 设置影响；路径范围规则可按匹配文件按需加载。官方也明确说明这些文件提供上下文，并非强制执行配置。 | Claude Adapter 不能仅把 `AGENTS.md` 改名为 `CLAUDE.md`；必须生成能力声明，并在版本/设置不足时给出兼容性提示。 |
| [Cursor: Rules](https://docs.cursor.com/context/rules) | **待确认。** 本轮请求已获得页面，但未取得足以可靠核对具体字段、目录层级与版本行为的稳定正文。 | Cursor Adapter 的文件格式、glob 语义与加载时机不应在 MVP 中据讨论文本硬编码；列为 P0 官方资料复核项。 |
| [GitHub Blog: How to write a great AGENTS.md](https://github.blog/ai-and-ml/github-copilot/how-to-write-a-great-agents-md-lessons-from-over-2500-repositories/) | **待确认。** 用户提供该链接及“2500+ 仓库、六领域”摘要；本轮页面抓取遇到传输失败，未独立验证统计口径和细节。 | 可作为写作启发，不作为产品验收要求或统计事实；P0 调研时重取并引用。 |

## 社区项目参考

以下结论只描述各 README 在本轮固定提交所宣称的功能；没有安装、执行、代码审计或安全背书。

| 项目与固定提交 | README 自述的相关能力 | 局限与可借鉴点 |
| --- | --- | --- |
| [markoblogo/AGENTS.md_generator@163042a](https://github.com/markoblogo/AGENTS.md_generator/tree/163042a866d080be2b435768d61c35064fe79292) | **社区声明。** 提供 `agentsgen init . --defaults --autodetect`、`check . --ci` 和 `update --dry-run --print-diff`；README 说明检查读取文件而不运行项目命令，并生成 `AGENTS.md`、`RUNBOOK.md` 与 `.agentsgen.json`。 | 可参考“安全更新、预览差异、静态检查不冒充命令运行”的边界。需在引入前另行审计许可证、维护状态、代码和依赖。 |
| [Paldom/agents-init@024bd01](https://github.com/Paldom/agents-init/tree/024bd01b566eb6badacaf9a37ced4c2262b4807f) | **社区声明。** README 描述一次生成 `AGENTS.md`、`CLAUDE.md`、MCP、子代理与 Skills，并提供 `--dry-run`、`--no-skills`、`--no-mcp` 与 `--no-claude`。 | 可参考“扫描前预览”和外部能力可关闭的设计；它的第三方 Skills/MCP 来源不应被本项目自动信任或安装。 |
| [RedLucky/agent-initiator@613f2b2](https://github.com/RedLucky/agent-initiator/tree/613f2b2ba72070bfd471b8170a6721285555aca9) | **社区声明。** README 宣称技术栈检测、每包 `AGENTS.md`、规则和 Skills，并提供 `status` 与保护已编辑文件的 `--upgrade`；README 同时写明尚未发布到 npm。 | 可参考 Monorepo 范围与受管文件状态模型；“未发布 npm”只是该提交 README 的状态，使用前须重新核实。 |

## 本轮结论与限制

- Codex 目录级 `AGENTS.md`/`AGENTS.override.md` 的优先级、当前目录截点和 32 KiB 默认上限有官方依据，适合作为 Codex MVP 的明确兼容目标。
- Claude Code 的当前文档已经支持读取 `AGENTS.md`，但由设置、版本与其自身 `CLAUDE.md`/规则机制影响。产品应以能力描述和诊断替代“所有工具加载方式相同”的假设。
- 未对 `agentsgen`、`agents-init`、`agent-initiator` 执行安装、构建、扫描或安全审计；不能据此把任何一项写入默认依赖、默认下载源或产品承诺。
- 未核实 Cursor 的细节、GitHub 博文的统计和任何第三方项目的许可证/漏洞/维护状态。它们都应进入实现前 P0 调研清单。

## 讨论来源与历史提议

本文件集以用户于 2026-11-20 提供的完整沟通材料为需求输入，归纳了工程宪法、四层治理、15 个规则域、CLI + 规则源 + Adapter + Skills + 持续审计等方案。原讨论中的建议和命令示例属于需求/方案材料，未经本轮核实的部分不作为当前产品事实。

| 用户提供的补充来源 | 本轮处理 |
| --- | --- |
| [OpenAI: Custom Code Review rules for Codex](https://developers.openai.com/blog/custom-code-review-rules-for-codex) | 待确认；保留审查规则参考入口，不以此宣称已配置审查或平台门禁 |
| [multica-ai/andrej-karpathy-skills](https://github.com/multica-ai/andrej-karpathy-skills) | 待确认；仅记录“长期行为原则可沉淀为可复用规则”的讨论来源 |
| [zichuanxu/andrej-karpathy-skills](https://github.com/zichuanxu/andrej-karpathy-skills) | 待确认；未核实当前 AGENTS.md 移植内容、许可证与维护状态 |
| [skydr1ft/andrej-karpathy-skills](https://github.com/skydr1ft/andrej-karpathy-skills) | 待确认；记录“一套原则、多工具输出”的设计启发，不承诺当前适配列表 |

讨论中的 `enterprise-agent-engineering`、`Agent Project Engineering`、`agent-project` 均为历史命名建议，`ai-agent-project-bootstrap` 为现有远程仓库名称；新版文档统一采用 Oh My Blueprint / `ombp`，目标仓库为 `oh-my-blueprint`，实际远程尚未更名。早期 Skills/MCP 自动发现、全工具首期支持和宽泛 scan 探测的建议，已收敛为分阶段交付、推荐与安装分离、静态扫描与命令执行分离。

用户后续提供了命名排除表和“公开搜索未发现精确同名项目”的判断，本轮未独立重查 GitHub、包注册表、域名和商标，因此均作为用户提供的历史检索结论保留，不表示已验证名称唯一或可注册。采用的品牌与命名空间见[命名规范](08-Naming.md)。

## P0 调研清单

1. 固定并记录 Codex、Claude Code、Cursor 各自支持版本、规则文件发现顺序、大小或上下文限制、目录范围和 Skills 路径。
2. 为每个 Adapter 写可重复的最小兼容夹具，使用实际工具版本验证生成文件是否加载，并保留命令与输出证据。
3. 对任何拟采用的外部 Skills、MCP、模板或生成器做来源、提交哈希、许可证、内容安全和依赖审计；批准前不得自动安装。
4. 为引用的 GitHub 博文重新获取正文，核对统计样本、发布日期和“六领域”原文表述。
5. 在规则 Schema 和 CLI 行为定稿前，重新访问上表官方资料；指令文件机制及产品版本可能发生变化。
