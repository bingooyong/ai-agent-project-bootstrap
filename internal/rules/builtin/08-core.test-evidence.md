---
schema_version: "1.0"
id: core.test-evidence
title: 测试与真实证据
version: "1.0.0"
category: testing
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
  description: 测试与真实证据的实际执行与适用证据需由任务审查确认
---
评估每次修改的测试影响。新功能验证目标行为，缺陷补可复现回归，权限含负向、接口含异常及边界，数据变更含适用并发一致性。记录实际命令、环境、退出码与证据；未执行标NOT_RUN，失败不隐瞒，静态或Mock结果不冒充真实集成验收。
