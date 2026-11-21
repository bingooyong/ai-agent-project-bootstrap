---
schema_version: "1.0"
id: core.delivery-report
title: 最终交付与规则维护
version: "1.0.0"
category: documentation
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
  description: 最终交付与规则维护的实际执行与适用证据需由任务审查确认
---
交付报告说明完成范围、修改文件、核心方案、实际命令与结果、未完事项、风险及需人工决策项。长期已验证约束维护到适用规则，避免重复与层级冲突；一次性任务要求和未知事实不自动固化，无执行证据不得宣称PASS。
