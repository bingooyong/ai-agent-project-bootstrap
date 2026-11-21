---
schema_version: "1.0"
id: core.database-safety
title: 数据库与数据安全
version: "1.0.0"
category: database
severity: P1
source:
  package: ombp-core
scope:
  modules: ["*"]
  paths: ["**/*"]
when: {}
overridable: false
dependencies: []
conflicts: []
constraints: []
enforcement: [instruction, manual-review]
validation:
  kind: manual
  description: 数据库与数据安全的实际执行与适用证据需由任务审查确认
---
数据库结构和数据变更需评估兼容、备份、恢复、幂等、数据丢失、索引、锁与事务并发。SQL参数化；测试用隔离数据库。生产写入只在明确目标和范围授权下执行，不能修改测试数据掩盖业务缺陷。
