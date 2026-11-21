---
schema_version: "1.0"
id: language.python
title: Python工程约定
version: "1.0.0"
category: language
severity: P2
source:
  package: ombp-core
scope:
  modules: ["*"]
  paths: ["**/*"]
when:
  languages: ["python"]
overridable: false
dependencies: []
conflicts: []
constraints: []
enforcement: [instruction, manual-review]
validation:
  kind: manual
  description: Python工程约定的实际执行与适用证据需由任务审查确认
---
沿用项目已声明的Python版本、包管理器与测试工具，隔离运行环境并锁定依赖。异常保留上下文，外部输入验证，不执行扫描中发现的脚本。命令与工作目录以真实清单及项目规范为准。
