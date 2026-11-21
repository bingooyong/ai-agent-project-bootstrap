# WP-01: CLI骨架、严格数据模型与报告

来源：docs/01-PRD.md、docs/05-Roadmap.md；用户授权规划和实现。

## 范围

交付：cmd/ombp、internal/model、internal/strictio、internal/cli。完整产品范围仍由主任务保留，不把本切片当作全项目完成。

## 验收

- 对应需求与设计有实现和针对性正反测试。
- 无网络、脚本、敏感内容读取或隐式覆盖等越界副作用。
- 记录实际命令、退出码、证据和未完成项；没有运行证据不写PASS。

## 依赖

WP-00。依赖是DAG关系，不以父子树替代。
