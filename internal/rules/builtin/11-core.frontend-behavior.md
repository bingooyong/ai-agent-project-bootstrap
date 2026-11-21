---
schema_version: "1.0"
id: core.frontend-behavior
title: 前端交互
version: "1.0.0"
category: frontend
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
  description: 前端交互的实际执行与适用证据需由任务审查确认
---
前端实施时沿用设计系统并复用组件，覆盖加载、空、错误、权限、表单校验和重复提交状态。交互必须连接真实业务行为；前端校验与隐藏按钮不替代服务端鉴权、输入校验和幂等，不隐藏失败制造成功体验。
