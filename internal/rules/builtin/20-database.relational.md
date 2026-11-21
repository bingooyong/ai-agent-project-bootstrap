---
schema_version: "1.0"
id: database.relational
title: 关系型数据库约定
version: "1.0.0"
category: database
severity: P2
source:
  package: ombp-core
scope:
  modules: ["*"]
  paths: ["**/*"]
when:
  databases: ["mysql", "postgresql", "oracle"]
overridable: false
dependencies: []
conflicts: []
constraints: []
enforcement: [instruction, manual-review]
validation:
  kind: manual
  description: 关系型数据库约定的实际执行与适用证据需由任务审查确认
---
已声明数据库驱动只证明依赖线索，不代表实例可连接。迁移与查询按实际数据库版本和隔离环境验证，评估事务、锁、索引、执行计划及兼容恢复；不得从扫描配置推断生产操作授权。
