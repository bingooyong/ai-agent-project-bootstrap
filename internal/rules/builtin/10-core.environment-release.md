---
schema_version: "1.0"
id: core.environment-release
title: 环境与发布
version: "1.0.0"
category: deployment
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
  description: 环境与发布的实际执行与适用证据需由任务审查确认
---
明确DEV、TEST、UAT、PROD。默认仅本地或已授权测试环境；生产连接、配置、部署、重启和迁移需要明确目标与范围授权。发布有实际验证、制品版本追溯和恢复方案，不把测试配置带入生产。
