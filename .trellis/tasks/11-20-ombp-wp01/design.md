# WP-01 设计入口

按docs/02-Architecture.md、03-Rule-Engine-Design.md、04-CLI-SDD.md实现。

边界：cmd/ombp、internal/model、internal/strictio、internal/cli；不修改其他工作包责任文件。

接口以docs/implementation/CONTRACTS.md为准，进入执行前细化本工作包的函数、状态和测试。后续阶段保留PLANNED，不预报能力完成。

## 实施接口与范围细化

以docs/implementation/CONTRACTS.md的共享Go API和数据字段为准。事实值与人工声明分开；声明命令与NOT_RUN执行状态分开。读文件以逐组件拒绝链接、非阻塞打开非普通文件为前提。
