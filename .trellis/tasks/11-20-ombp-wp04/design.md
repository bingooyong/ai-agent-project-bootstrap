# WP-04 设计入口

按docs/02-Architecture.md、03-Rule-Engine-Design.md、04-CLI-SDD.md实现。

边界：internal/renderer、internal/adapters；不修改其他工作包责任文件。

接口以docs/implementation/CONTRACTS.md为准，进入执行前细化本工作包的函数、状态和测试。后续阶段保留PLANNED，不预报能力完成。

## 冻结接口

renderer.File{Path, Content string}; Render(model.Profile,rules.Resolution)([]File,error)。按实际Module.Path聚合，不创建虚假目录，根与目录文件稳定排序；先命令，再规则/证据导航。20份规则源非复制模板；特殊作用域在指令中明确。不支持的能力/累计超过32KiB阻断，已有AGENTS.override由高层阻断。
