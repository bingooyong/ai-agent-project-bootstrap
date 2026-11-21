# 规则引擎设计

版本：V0.2；状态：拟议协议。覆盖 FR-003～007、NFR-003/004/007/008。

## 1. 规则模型（D-RULE）

规则文件采用严格 YAML Front Matter + UTF-8 Markdown 正文。字段类型与作用域、依赖、约束关系需分别校验。2026-11-21 的实现采用严格类型解码与 Go 业务校验，并提供由类型生成的数据形状 Schema；未引入通用 JSON Schema 执行器。以下示例说明协议，具体实现与验证以代码和任务证据为准。

```yaml
---
schema_version: "1.0"
id: security.production-authorization
title: 生产操作必须有明确范围授权
version: "1.0.0"
category: security
severity: P0
source:
  package: ombp-core
scope:
  modules: ["*"]
  paths: ["**/*"]
when:
  project_types: ["*"]
overridable: false
dependencies: []
conflicts: []
constraints:
  - key: operations.production.authorization
    operator: requires
    value: explicit-scoped-authorization
enforcement: [instruction, manual-review]
validation:
  kind: manual
  description: 核对实际操作目标、范围及有效授权记录
---
连接或修改生产环境前，确认明确授权的目标、动作和范围。
扫描、规则生成及本地文件写入均不授予生产权限。
```

| 字段 | 约束 |
| --- | --- |
| `id`、`version` | ID 稳定、命名空间唯一；内容改变必须改变版本；同 ID+版本而内容摘要不同报完整性错误 |
| `severity` | P0 禁止未授权高风险行为；P1 强制流程；P2 推荐；P3 场景建议；不是覆盖优先级 |
| `scope`、`when` | 明确模块、相对路径和已支持的事实谓词；不支持的谓词不得当作匹配成功 |
| `dependencies` | MVP 使用明确 ID 和精确版本；范围版本解析后续引入，避免隐式选择最新版 |
| `conflicts` | 显式冲突 ID，冲突集有交集作用域时阻断 |
| `overridable` | 仅声明可否走显式替代流程；`false` 不得由低信任来源改写 |
| `constraints` | 有类型的键、操作符和值；首期只支持相等、要求、禁止以及集合允许关系 |
| `enforcement` | 区分 instruction/static/manual/外部 CI；声明 ci 不代表已建立 CI 门禁 |
| `validation` | 固定校验器 ID 或人工验证说明；禁止在元数据中嵌入任意可执行脚本 |
| 正文 | 可被渲染的工程指导文本；外部正文不能修改本工具权限和解析流程 |

规则等级 P0～P3 与 finding 的 Critical/High/Medium/Low/Informational 分别建模。Critical 可用于阻断完整性风险，不能把所有 P0 规则存在误报成 Critical 问题。

## 2. 来源、优先级与例外（D-RESOLVE）

源层次为平台核心、组织包、个人偏好、项目规则、技术栈、目录规则。此顺序表达候选收集和可追溯上下文，**不采用最后一个文件覆盖前一个文件的算法**。技术栈与目录层通常添加细则；范围越小也不能削弱受保护约束。

每个来源记录包 ID、版本、来源 URI/本地相对路径、revision、摘要和信任状态。MVP 只读取随二进制提供的规则包及项目明确配置的本地规则，不扫描个人全局目录来隐式接入规则，不联网解析最新版。

同 ID 仅在显式 override 记录通过校验时可替代；否则不同版本或不同正文冲突阻断。不同 ID 先按结构化约束和显式 conflicts 检测，正文无法判定的语义差异进入人工审查项，不宣称冲突检查完备。

P0 不提供项目弱化机制。P1 在未来企业治理阶段可支持有签发者、责任人、原因、范围、到期时间、补偿控制和撤销记录的例外；MVP 没有可信签发与校验机制，故拒绝弱化 P1。P2/P3 仍须明确 override、被替代版本和作用域，不能通过编辑输出文件规避解析。

MVP 从 `.ombp/rules/overrides/*.yaml` 读取 `schema_version`、`rule_id`、`before`、`after`、`scope`、`owner` 和 `reason`。`before` 与 `after` 均包含规则版本和规范化正文摘要 `sha256`，对应给定 ID 仅有的两个不同版本。旧规则必须允许替代，两侧均为 P2/P3，且作用域和适用条件完全一致。每个 ID 只允许一条直接替代记录；已经写入项目锁的规则版本仍需专门的升级流程。`owner` 和 `reason` 用于本地追溯，不代表签发或长期授权。

用户会话对具体本地写入、Git 提交等给出的有效授权可用于相应动作，无需重复批准；该授权不自动变成永久放宽规则的 override。受保护规则的调整需走规则源版本评审，而不是让 Agent 在目标工程中自我授权。

## 3. 确定性解析算法

1. 校验配置、Schema 主版本、锁和包摘要。未知主版本、重复键、同版本异内容直接失败。
2. 加载画像事实、人工配置和现有规则归属；未知事实保留 unknown，不匹配依赖该事实的专用规则。
3. 收集基础规则和匹配条件满足的候选规则；显式人工选择可加载技术栈规则，但须保留“人工声明”来源。
4. 求依赖闭包，校验精确版本、缺失依赖、禁用的必需规则和循环依赖。
5. 处理经过允许的 override；记录替代链及范围。删除受保护规则或不完整例外均阻断。
6. 计算作用域交集，检查显式冲突及已支持的结构化约束，输出规则 ID、证据和冲突路径。
7. 同 ID+版本+摘要去重，保留多个来源；不同 ID 的相似正文只报告冗余线索，不自动删除。
8. 依赖拓扑排序，等序节点按层次、作用域规范路径和 ID 排序；生成 canonical resolved manifest 与摘要。
9. 适配器检查可表达性、文件加载预算和必要规则覆盖；不支持或被截断的必需规则阻断。
10. 渲染、生成计划；有阻断问题不得生成可应用计划，仅输出诊断与建议。

结构化约束例：`tools.shell.execution = forbidden` 与相同作用域的 `tools.shell.execution = required` 互斥；“数据库迁移应审查”与“迁移建议简化”没有充分结构信息，不能仅凭关键词判定等价或冲突。`requires explicit authorization` 不等于永久 `forbidden`。

MVP 的作用域匹配统一使用 `/` 相对路径和限定的 `*`、`**`、`?` glob；不支持任意正则、外部函数或否定表达式。可计算交集则进行检查，无法可靠确定交集时保守报告 `scope-overlap-unknown`，涉及受保护约束时阻断，而不是假设互不重叠。

## 4. 统一源与输出归属（D-OWN）

目标工程的统一源为锁定包引用和 `.ombp/rules/custom/`，不能以 `AGENTS.md` 作为唯一原始存储。用户写在受管区之外的内容继续保留，但必须记录为未编译手工指令；发现疑似冲突时报告，不能声称跨工具已完全一致。

受管文件采用稳定区块 ID：

```markdown
<!-- ombp:begin id=project-core schema=1 -->
<!-- generated from locked rules; edit custom sources to change this block -->
...生成内容...
<!-- ombp:end id=project-core -->
```

标记不包含自己的内容哈希，避免自引用。lock 分别记录规范源指纹、受管区字节摘要、生成基线和完整文件摘要；计划使用完整文件摘要防止遗漏手工区的并发编辑。

| 当前状态 | 默认行为 |
| --- | --- |
| 文件不存在 | 计划创建，apply 时再次断言不存在 |
| 已有文件且无归属标记 | 保留原文，输出冲突；不自动插入区块或接管 |
| 标记与 lock 相符，受管内容无改动 | 可生成区块更新计划；手工区保持逐字节不变 |
| 受管区被人工修改 | 保留现场，输出三方差异；MVP 不自动合并 |
| 仅手工区修改 | 新扫描保留新手工区后生成新计划；旧计划因整文件哈希变化失效 |
| 标记重复/嵌套/缺失或 lock 缺失 | 归属不明，阻断；需独立迁移设计 |
| 旧目录已删除 | 报漂移；不删除用户目录。后续显式删除计划只可处理未修改的工具专属文件 |

MVP 不提供无条件 `--force` 或自动 adopt。用户可先把旧规则整理进 custom 源并保留原文件，再以专门审核的迁移方案接管；未交付该能力前，只输出候选，不覆盖旧文件。

## 5. 版本、锁与可复现性（D-LOCK）

ProjectLock 至少包含 `schema_version`、`tool_version`、`profile_fingerprint`、`config_fingerprint`、规则包/规则/适配器/模板精确版本及 SHA-256、resolved manifest、输出文件归属和摘要。若支持 Skills，再增加来源 revision 和内容摘要。

```yaml
schema_version: "1.0"
tool_version: "<release-version>"
rules:
  - id: security.production-authorization
    version: "1.0.0"
    source: ombp-core
    sha256: "<content-digest>"
outputs:
  - path: AGENTS.md
    blocks:
      - id: project-core
        sha256: "<managed-block-digest>"
```

以上占位值不可用于真实 lock。路径和内容哈希分别处理：规则正文统一 UTF-8/LF，受管新内容使用 LF；现有手工区保留原字节。已有混合换行导致无法安全分区时报告冲突。路径拒绝绝对路径、`..`、Windows 盘符/UNC、大小写冲突和非法保留名；具体平台支持矩阵需测试证明。

规范序列化排序所有无序集合、固定键顺序，摘要使用版本化 canonical JSON。扫描时间在报告元数据中，生成正文和输入指纹不含当前时间、绝对目录或不稳定遍历顺序。人工例外到期检查使用显式评估时间并记录，以便重放。

`check` 不升级规则、不更新 lock。锁定包缺失且离线不可得时明确失败；不联网补齐。Schema 迁移、规则升级、模板升级必须重新规划并展示差异。

## 6. 更新事务与恢复（D-TX）

`update` 复用初始化流水线，增加旧基线对比。首次 MVP 的 init/apply 已需以下事务保障；Phase 2 才提供面向版本漂移的完整 update 命令。

```text
读取旧锁/基线 → 重新扫描 → 解析并渲染 → 生成计划
→ 显式 apply → 互斥锁 → 复核所有输入/路径/归属
→ 备份与 journal → 分文件替换 → 最后替换 lock
→ 校验输出 → 标记事务完成
```

计划记录 target root、输入和输出旧摘要、新摘要、动作、计划内容摘要与 Schema。apply 不信任计划提供的任意目标路径或内容：重新解析当前受信规则并渲染，比对预期结果。计划不得包含可执行命令。

文件写入前后再次验证路径和摘要，禁止跟随输出符号链接；目录互斥只约束本工具实例，不能阻止其他编辑器。实现须验证各平台的安全文件句柄/替换语义；不支持安全操作的文件系统明确拒绝应用。不能把字符串路径清理宣称为已解决全部 TOCTOU 风险。

恢复日志记录每一步及已写摘要。崩溃后不得基于新 lock 假设所有输出已写齐；检查 journal。回滚对原文件按原摘要恢复，对新建文件仅在仍为本事务内容时删除；任何第三方修改均阻断自动恢复并提供逐文件说明。没有日志时，只有能确认已退出的写入者才会进入孤立锁预览；应用需核对绑定项目根和锁内容的摘要，只移除该文件。state 的备份权限受限，保留策略后续单独确定，恢复前不自动清理。

## 7. Check、Audit 与持续维护（D-GOV）

`check` 运行确定性静态校验，按指纹快速发现规则/命令/路径/生成区变化；完整遍历触限时不输出“无问题”。`audit` 在 Phase 2 增加更广覆盖的漂移与人工审查线索，Phase 5 才可增加可选语义模型建议。

每条 finding 包含稳定 `code`、等级、规则 ID、相对文件定位、证据摘要、影响、修复建议、确认需求和检测方法。未知命令来源应标 unknown；禁止将“没有检测到冲突”等同“所有规则一致”。

维护触发包括清单、锁文件、模块、测试、CI、规则源和适配器版本变化。CI 默认只检查并阻断，不自动改写或提交规则。一次性任务约束、未经证实的架构推测和敏感业务信息不自动写成长期规则。
