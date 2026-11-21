---
schema_version: "1.0"
id: core.api-compatibility
title: 接口兼容
version: "1.0.0"
category: api
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
  description: 接口兼容的实际执行与适用证据需由任务审查确认
---
接口修改前检索调用方，保护字段类型、语义、默认值与版本兼容。事件、消息、缓存、配置和持久化格式也属契约；新增入口验证鉴权、输入与错误路径，不静默删除或改写已有字段。
