# WP-06 设计入口

按docs/02-Architecture.md、03-Rule-Engine-Design.md、04-CLI-SDD.md实现。

边界：internal/governance；不修改其他工作包责任文件。

接口以docs/implementation/CONTRACTS.md为准，进入执行前细化本工作包的函数、状态和测试。后续阶段保留PLANNED，不预报能力完成。

## 2026-11-21 集成细化

只读Check扫描当前配置/锁/画像/命令来源/托管输出，Finding保留规则与位置证据、影响和建议。完整性冲突4、不完整3、质量1、静态通过0；不写状态、不恢复、不执行命令。临时真实初始化工程为基线，篡改/缺失/漂移/unknown场景做正反例。接口见CONTRACTS与internal/governance。
