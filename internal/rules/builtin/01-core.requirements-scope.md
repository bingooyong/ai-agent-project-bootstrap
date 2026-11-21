---
schema_version: "1.0"
id: core.requirements-scope
title: 需求与范围
version: "1.0.0"
category: engineering
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
  description: 需求与范围的实际执行与适用证据需由任务审查确认
---
先阅读需求与实际业务流程，区分明确需求、必要技术修改与自主优化。只实施已授权范围；歧义保留假设及待确认项，不擅自删除功能、扩展接口或重构模块。
