# MVP 静态扫描支持范围

六个合成工程覆盖 AC-01 的主要技术栈与未知场景。测试用具体文件、定位键和 SHA-256 核对声明来源，同时比较模块列表、命令参数、工作目录与执行状态。用例见 [scanner fixture matrix](../../internal/scanner/matrix_test.go)，混合工程、异常清单、预算和敏感输入用例见 [scanner regression tests](../../internal/scanner/scanner_test.go)。

| 样本 | 实测识别 | 命令及边界 |
| --- | --- | --- |
| Go 多模块 | 根和 `tools` 各自以 `go.mod#module` 入册。PostgreSQL、MySQL 分别定位到各自清单的第 3 行。 | 各模块有工作目录匹配的 `go build ./...` 和 `go test ./...` 候选。 |
| Python API | `pyproject.toml` 的依赖声明识别 FastAPI 和 Oracle，pytest 配置定位到 `tool.pytest.ini_options`。混合工程另覆盖 PostgreSQL 和多行依赖。 | 检出了 pytest 声明，命令列表为空。动态字段和间接 requirements 保留待确认。 |
| JavaScript | `package.json` 声明 JavaScript，Jest 定位到 `/devDependencies/jest`，未出现 TypeScript 或 React 事实。 | 根目录 `npm run test` 来源是 `/scripts/test`。 |
| TypeScript 和 React 工作区 | `web/package.json` 的依赖键定位 React 和 TypeScript，`web/tsconfig.json#/compilerOptions` 提供第二条 TypeScript 证据。根和 `web` 是两个模块。 | `web` 继承显式工作区包管理器，得到 `pnpm run build` 和 `pnpm run test`。包管理器冲突时省略调用。 |
| 空目录 | 画像覆盖为 `complete`，技术栈为未知，模块和命令为空。 | 无候选命令。 |
| 未知工程 | `backend`、`frontend`、JSX 片段和通用 SQL 均未触发技术栈或 Oracle 判断。 | 无候选命令。 |

Go 和 TOML 解析支持声明子集。重复 Go `go`/`toolchain` 单例指令、非法版本/工具链或 `require` 版本语法、嵌套顶层版本声明、缺少模块声明和损坏的依赖块会使扫描不完整；有效但未支持的扩展指令会保留 unknown。Go 版本语法遵循本地 Go 解析器的接受形式；工具链只做语法识别，不检查当前安装版本。依赖版本 token 校验完整或缩写的 semver 主版本形式、预发布和构建元数据，暂不校验模块路径与 `/vN` 主版本的兼容性。Go CLI 会拒绝重复版本指令，OMBP 单元、macOS CLI 与 Linux arm64 CLI 都验证为 incomplete，详见[集成证据](evidence/2026-11-21-go-mod-validation-01/results.json)和 [Linux 原生证据](evidence/2026-11-21-go-mod-validation-linux-02/results.json)；指令与依赖版本边界回归见[Go require 版本证据](evidence/2026-11-21-gomod-require-version-01/results.json)。TypeScript JSONC、动态 Make 定义、间接 requirements 和未支持的工作区写法可能需要人工确认。Node 命令以脚本键为首条证据，其后记录参与选择的 `packageManager` 字段、锁文件或工作区匹配条目；隐式 npm 仅作为调用约定。数据库客户端和框架依赖仅证明清单声明。所有命令在画像中均标为 `NOT_RUN`。

本地使用 Go 1.27.3 在 macOS 26.6.2 arm64 上执行 scanner 全包 race 测试。矩阵通过 6/6，包语句覆盖率为 81.9%，退出码为 0，运行期间源码摘要一致。覆盖率统计针对代码语句，真实工程识别精度尚未测量。[原始命令、输入摘要与输出](evidence/2026-11-21-scanner-matrix-01/scanner-race-coverage.json)

```sh
GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local go test -race -count=1 -cover -v ./internal/scanner
```

## WP-02 资源实测

使用本地当前源码构建 macOS CLI。合成工程包含 100 个目录、10,000 个普通文件和 4 份受支持清单，以及敏感路径、依赖目录和未执行脚本的验证输入。下表记录 CLI 子进程的总耗时及 macOS `/usr/bin/time -l` 报告的峰值常驻内存。

| 样本 | 退出码 / 覆盖 | 耗时 | 峰值 RSS |
| --- | --- | --- | --- |
| 10,109 条，3 次 | 0 / complete | 179–522 ms | 13.41–13.61 MiB |
| 恰好 20,000 条 | 0 / complete | 341 ms | 18.64 MiB |
| 20,111 条 | 3 / incomplete | 346 ms | 17.84 MiB |
| 同一超限样本，显式排除 `bulk` | 0 / complete | 188 ms | 14.08 MiB |

超限样本在已计数 19,907 条时发现下一批目录项无法纳入剩余预算，提前中止，清单读取量为 0。正常、恰好触限和显式排除场景均只读取 4 份清单（197 字节），确认敏感和依赖目录排除，并校验清单摘要。数据用于建立此设备上的合成基线，未设跨设备性能门槛。首轮因错误假设计数必须达 20,001 未通过，保留[首轮记录](evidence/2026-11-21-scanner-budget-01/results.json)；[复测记录](evidence/2026-11-21-scanner-budget-02/results.json)包含完整输出、各条命令与源码摘要。

```sh
python3 scripts/verify_scanner_budget.py --out docs/05-testing/evidence/scanner-budget-local
```
