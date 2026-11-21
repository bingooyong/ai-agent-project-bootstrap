---
schema_version: "1.0"
id: core.git-authorization
title: Git操作授权
version: "1.0.0"
category: git
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
  description: Git操作授权的实际执行与适用证据需由任务审查确认
---
修改前后检查git status和相关diff，显式选择暂存路径。Commit、Push、Merge、Tag、Release、删除分支、改历史、Force Push、reset --hard及clean -fd按明确授权执行；已有具体有效授权可复用，但不得扩大到其他仓库和变更。
