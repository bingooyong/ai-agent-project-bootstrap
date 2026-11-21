# Oh My Blueprint · OMBP

**Agent Engineering Blueprint — One Blueprint. Every Agent.**

本地优先的 AI Agent 工程初始化与规则治理工具。根据工程文件生成可追溯画像，匹配版本化工程规则，逐步交付配置生成、持续维护、多 Agent 适配与 Skills 管理。

## 开发

```sh
make build
./build/ombp version --format json
./build/ombp scan . --format json
./build/ombp rules list --format json
./build/ombp rules validate . --format json
make check
```

Go module 为 `github.com/bingooyong/oh-my-blueprint`，最低版本设计为 Go 1.25，本轮本地工具链为 Go 1.27.3。当前严格项目读取在 macOS/Linux 实现；Windows 会明确拒绝，不因交叉编译成功宣称安全读取支持。

扫描只读取受限的声明文件，不执行项目脚本，不访问网络或模型，不读取凭证内容。返回的命令是静态声明/候选，执行状态为 `NOT_RUN`。未知或超预算结果不会升级为完整确认。Python/Go 的声明识别支持矩阵与后续缺口随实施报告维护。

目前只交付了部分离线核心；配置应用、完整 Check/Update、多 Agent、Skills、AI 和企业治理按 [Trellis 实施 DAG](docs/implementation/task-dag.json)继续推进。命令帮助只展示真实可用入口，未实现功能不提供成功占位。

## 设计与实施

- [设计文档导航](docs/README.md)
- [实施契约](docs/implementation/CONTRACTS.md)
- [依赖选择与安全 API](docs/implementation/DEPENDENCIES.md)
- [适配器官方依据](docs/implementation/ADAPTER-EVIDENCE.md)

目标仓库名是 `oh-my-blueprint`；实际远程更名、包注册和发布尚未执行。AGENTS.md 提供行为指引，硬权限与 CI 门禁仍需工具和平台保障。
