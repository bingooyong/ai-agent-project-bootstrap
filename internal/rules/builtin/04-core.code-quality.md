---
schema_version: "1.0"
id: core.code-quality
title: 代码质量
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
  description: 代码质量的实际执行与适用证据需由任务审查确认
---
沿用仓库代码风格，保持职责明确与错误可观察。禁止吞掉异常、空实现、硬编码假成功，以及删除或弱化测试断言、关闭校验或类型检查来制造通过。
