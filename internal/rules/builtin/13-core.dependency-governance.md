---
schema_version: "1.0"
id: core.dependency-governance
title: 依赖治理
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
  description: 依赖治理的实际执行与适用证据需由任务审查确认
---
新增依赖前先查已有能力，评估必要性、可信来源、许可证、维护状态与已知漏洞。核心升级有明确授权与兼容验证，固定版本和校验摘要以便复现；离线或过期情报不能证明没有安全风险。
