---
schema_version: "1.0"
id: core.documentation-consistency
title: 文档一致性
version: "1.0.0"
category: documentation
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
  description: 文档一致性的实际执行与适用证据需由任务审查确认
---
变更后判断README、需求、架构、接口、数据库、配置、部署、测试和变更记录是否需同步。需求到设计、代码、测试、验收可追溯；当前文档与历史来源区分时点，不重写历史证据伪造从未失败。
