---
schema_version: "1.0"
id: core.pre-change-investigation
title: 修改前调查
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
  description: 修改前调查的实际执行与适用证据需由任务审查确认
---
修改前阅读相关实现、调用方、数据模型、测试和文档，并检查 git status。保护用户已有未提交与未跟踪内容，优先复用仓库模式，不能仅按文件名或注释推断系统行为。
