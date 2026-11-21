---
schema_version: "1.0"
id: language.golang
title: Go工程约定
version: "1.0.0"
category: language
severity: P2
source:
  package: ombp-core
scope:
  modules: ["*"]
  paths: ["**/*"]
when:
  languages: ["go"]
overridable: false
dependencies: []
conflicts: []
constraints: []
enforcement: [instruction, manual-review]
validation:
  kind: manual
  description: Go工程约定的实际执行与适用证据需由任务审查确认
---
沿用当前Go module与包边界，main保持薄入口；gofmt、显式错误与context取消、同包目录测试、窄接口由调用方定义。使用画像中有证据的命令及工作目录，不为套用模板新增module、框架或抽象。
