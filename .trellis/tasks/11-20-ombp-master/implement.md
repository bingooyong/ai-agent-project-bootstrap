# 完整实施顺序

按docs/05-Roadmap和task-dag依赖执行。WP-02/03在领域接口冻结后并行；WP-05先进行故障注入与安全设计评审。

当前先完成WP-00、WP-01及扫描/规则核心，再继续Codex渲染、事务与Check。其余阶段仍为本目标组成部分。

验证：go test -race -count=1 ./...、go vet ./...、gofmt、CLI进程E2E、文档/DAG上下文校验。跨平台原生能力未运行时明确NOT_RUN。
