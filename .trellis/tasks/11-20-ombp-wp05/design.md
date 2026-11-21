# WP-05 设计入口

按docs/02-Architecture.md、03-Rule-Engine-Design.md、04-CLI-SDD.md实现。

边界：internal/updater、internal/config；不修改其他工作包责任文件。

接口以docs/implementation/CONTRACTS.md为准，进入执行前细化本工作包的函数、状态和测试。后续阶段保留PLANNED，不预报能力完成。

## 事务细化

Build(root,toolVersion)生成绑定canonical root、扫描/config/custom输入、当前输出摘要的Plan；Apply不信任plan内容，重新Build并比较语义投影。仅输出AGENTS、实际module目录规则、.ombp/project.yaml、profile.json、lock.yaml。Existing未托管阻断，保留手工区字节。lock记录rule/adapter/template版本、managed block摘要。

Unix逐组件nofollow父fd写；absent用Linkat不覆盖。写journal intent/旧内容/旧新摘要并Sync，逐文件替换，lock最后。崩溃恢复检查所有intent（不限written标记），old/new/other条件恢复；人工新改动不覆盖。mutex只协调本工具；CAS与rename间恶意并发不是绝对隔离，不宣称断电硬件持久性。Windows暂拒绝安全I/O。
