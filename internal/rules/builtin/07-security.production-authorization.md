---
schema_version: "1.0"
id: security.production-authorization
title: 安全与敏感数据
version: "1.0.0"
category: security
severity: P0
source:
  package: ombp-core
scope:
  modules: ["*"]
  paths: ["**/*"]
when: {}
overridable: false
dependencies: []
conflicts: []
constraints:
  - key: operations.production.authorization
    operator: requires
    value: explicit-scoped-authorization
enforcement: [instruction, manual-review]
validation:
  kind: manual
  description: 安全与敏感数据的实际执行与适用证据需由任务审查确认
---
禁止提交或输出密钥、Token、密码、私钥和敏感客户数据。敏感入口服务端鉴权，按场景验证越权、注入、XSS、CSRF、SSRF及上传限制；不得绕过安全检查或向未批准服务外发代码和数据。连接或修改生产环境前必须有明确目标、动作及范围授权。
