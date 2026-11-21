---
schema_version: "1.0"
id: language.typescript
title: TypeScript工程约定
version: "1.0.0"
category: language
severity: P2
source:
  package: ombp-core
scope:
  modules: ["*"]
  paths: ["**/*"]
when:
  languages: ["typescript"]
overridable: false
dependencies: []
conflicts: []
constraints: []
enforcement: [instruction, manual-review]
validation:
  kind: manual
  description: TypeScript工程约定的实际执行与适用证据需由任务审查确认
---
沿用已配置的TypeScript严格程度、模块和构建体系，不用any、忽略指令或关闭类型检查掩盖契约缺陷。边界数据运行时校验；命令采用清单证据，不伪造typecheck/test脚本。
