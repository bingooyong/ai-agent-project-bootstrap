---
schema_version: "1.0"
id: core.adversarial-review
title: 对抗式审查
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
  description: 对抗式审查的实际执行与适用证据需由任务审查确认
---
审查不默认相信完成声明或历史PASS。从错误输入、权限绕过、并发、数据丢失、兼容、假实现、静默失败与测试有效性寻找问题。发现提供文件位置、证据或复现条件、原因、影响和修复建议；推测标待验证。
