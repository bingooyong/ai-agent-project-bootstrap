---
schema_version: "1.0"
id: core.architecture
title: 架构与模块边界
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
  description: 架构与模块边界的实际执行与适用证据需由任务审查确认
---
沿用当前分层、模块和依赖方向，先查已有组件再新增实现。避免循环依赖、复制实现和无消费者抽象；更换框架、运行时或基础设施须有明确任务范围及设计依据。
