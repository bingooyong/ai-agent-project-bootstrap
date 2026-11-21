# 当前验证状态

2026-11-21；交付状态 PARTIAL。

已实现受限manifest识别和预算/安全测试；Go/TOML静态子集与扩展样本覆盖需继续。

实际证据：[扫描支持矩阵](../../../docs/05-testing/scanner-support.md)、[scanner race 和覆盖率](../../../docs/05-testing/evidence/2026-11-21-scanner-matrix-01/scanner-race-coverage.json)、[资源边界运行](../../../docs/05-testing/evidence/2026-11-21-scanner-budget-02/results.json)、[最新集成](../../../docs/05-testing/evidence/2026-11-21-go-mod-validation-01/results.json)及 [Linux arm64](../../../docs/05-testing/evidence/2026-11-21-go-mod-validation-linux-02/results.json)。六组合成夹具通过；20,000 条目完整、超限返回 3/incomplete、排除后恢复；重复 `go` 或 `toolchain` 单例声明也返回 incomplete。未测真实工程识别精度。

2026-11-21 增补 Go 指令语法边界：[新回归证据](../../../docs/05-testing/evidence/2026-11-21-gomod-directive-syntax-01/results.json)。`go` 版本采用 Go 接受的形式；`toolchain` 限定 `default` 或 `go1` 前缀，仍不检查本机运行时兼容性。非法值和 require 块内的顶层 go 指令使扫描不完整。此证据为单元与全树测试，不是 CLI 或真实工程识别精度验收。

2026-11-21 增补 require 依赖版本语法：[新回归证据](../../../docs/05-testing/evidence/2026-11-21-gomod-require-version-01/results.json)。单行和块式 require 都校验 semver token；识别缩写版本、预发布、伪版本及 `+incompatible`。不匹配的语法会返回 incomplete。模块路径与 `/vN` 的一致性仍不在本解析子集。

完整目标仍按主任务和全部WP保留；没有标记完成或归档。
