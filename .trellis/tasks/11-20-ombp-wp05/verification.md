# 当前验证状态

2026-11-21；交付状态 PARTIAL。

预览/计划/应用/手工区/版本指纹/显式恢复已通过回归及进程中断测试；完整迁移/跨平台/恶意并发隔离未验收。

实际证据：[最新集成](../../../docs/05-testing/evidence/2026-11-21-doctor-transaction-02/results.json)、[Linux arm64](../../../docs/05-testing/evidence/2026-11-21-doctor-transaction-linux-01/results.json)。额外覆盖 `doctor` 对孤立写锁的阻断和显式清理后重新收敛。长期事务保留策略与 Windows 写语义仍开放。

完整目标仍按主任务和全部WP保留；没有标记完成或归档。

普通恢复在修改死进程锁之前校验日志归属；无日志的孤立锁需绑定项目根及锁记录的独立摘要。新增日志创建前和移除后两个真实退出窗口，覆盖保留有效 journal、活进程、错误摘要和跨工程拒绝。`doctor` 发现写锁后会阻断，对状态目录无法访问时报告 incomplete。见 [集成证据](../../../docs/05-testing/evidence/2026-11-21-go-mod-validation-01/results.json) 和 [Linux arm64 运行](../../../docs/05-testing/evidence/2026-11-21-go-mod-validation-linux-02/results.json)。长期事务保留策略未实施。
