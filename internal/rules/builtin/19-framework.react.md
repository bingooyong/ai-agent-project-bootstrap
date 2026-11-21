---
schema_version: "1.0"
id: framework.react
title: React组件约定
version: "1.0.0"
category: framework
severity: P2
source:
  package: ombp-core
scope:
  modules: ["*"]
  paths: ["**/*"]
when:
  frameworks: ["react"]
overridable: false
dependencies: []
conflicts: []
constraints: []
enforcement: [instruction, manual-review]
validation:
  kind: manual
  description: React组件约定的实际执行与适用证据需由任务审查确认
---
组件复用现有设计和状态管理模式，区分服务端数据与本地交互状态，处理取消与过期请求。覆盖加载、空、错误、权限和表单状态；用用户可观察行为测试组件，不复制服务端授权逻辑。
