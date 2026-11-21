# 依赖与安全 API 调研

日期：2026-11-20。范围：WP-00 的 Go CLI、配置解析、Schema 与文件边界选型。此文档记录只读调研与建议；实际采用版本以 `go.mod`、`go.sum` 及实施记录为准。本次未安装或升级工具，未运行漏洞扫描，不能据此声称依赖没有漏洞。

## 1. 建议版本

| 组件 | 建议 | 一手依据 | 决策理由与风险 |
| --- | --- | --- | --- |
| Go | 本机验证工具链为 `go1.27.3 darwin/arm64`；最低支持版本由主实施计划明确 | 本机 `go version`、`go doc os.OpenRoot`；[os 文档](https://pkg.go.dev/os#Root) | 使用标准库进行遍历、摘要、模板、JSON、文件 I/O 与测试；若采用 Root 新方法须核对最低 Go 版本，不用本机编译成功代替旧版兼容 |
| Cobra | 固定 `github.com/spf13/cobra v1.10.2` | [版本记录](https://proxy.golang.org/github.com/spf13/cobra/@v/v1.10.2.info)、[go.mod](https://github.com/spf13/cobra/blob/v1.10.2/go.mod)、[License](https://github.com/spf13/cobra/blob/v1.10.2/LICENSE.txt) | 适合已有多层命令协议、帮助、参数校验和可测试 I/O；Apache-2.0。框架默认行为需约束，不自动开启环境变量导入、补全命令或外部执行 |
| YAML | 固定 `go.yaml.in/yaml/v3 v3.0.6` | [版本记录](https://proxy.golang.org/go.yaml.in/yaml/v3/@v/v3.0.6.info)、[README](https://github.com/yaml/go-yaml/blob/v3.0.6/README.md)、[License](https://github.com/yaml/go-yaml/blob/v3.0.6/LICENSE)、[API 源码](https://github.com/yaml/go-yaml/blob/v3.0.6/yaml.go) | 官方 YAML 组织接续维护的 v3 稳定 API；旧 `go-yaml/yaml` 仓库已不维护，不继续新增旧导入路径。库容许的 YAML 特性比产品协议宽，必须由应用收紧 |
| Unix 安全文件访问 | 如最低支持 Go 1.25，固定 `golang.org/x/sys v0.47.0` | [go.mod](https://proxy.golang.org/golang.org/x/sys/@v/v0.47.0.mod)、[License](https://github.com/golang/sys/blob/v0.47.0/LICENSE)、[unix API](https://pkg.go.dev/golang.org/x/sys/unix) | Go Authors BSD-3-Clause；可提供 Darwin/Linux 的 Openat/O_NOFOLLOW/O_DIRECTORY/O_NONBLOCK，避免仅 Lstat 后路径打开的竞态。当前 latest v0.50.0 要求 Go 1.26；v0.48.0亦要求1.26，v0.47.0已确认要求1.25 |
| JSON Schema | 候选 `github.com/santhosh-tekuri/jsonschema/v6 v6.0.3`，仅在实现通用 Schema 校验时引入 | [版本记录](https://proxy.golang.org/github.com/santhosh-tekuri/jsonschema/v6/@v/v6.0.3.info)、[README](https://github.com/santhosh-tekuri/jsonschema/blob/v6.0.3/README.md)、[go.mod](https://github.com/santhosh-tekuri/jsonschema/blob/v6.0.3/go.mod)、[License](https://github.com/santhosh-tekuri/jsonschema/blob/v6.0.3/LICENSE) | Apache-2.0；支持 Draft 2020-12，go.mod 声明 Go 1.21，运行依赖包含 x/text。若首个切片只用严格类型与业务校验，不必提前引入；此时不能声称已完成通用 JSON Schema 验证 |

2026-11-20 的 Go 官方模块代理 `@latest` 返回值分别为 Cobra v1.10.2、YAML v3.0.6、jsonschema v6.0.3。代理给出的 VCS revision 分别为 `88b30ab89da2d0d0abb153818746c5a2d30eccec`、`5a62aba63b773192cd10ab954e21e143d7b00c7a`、`b0fc661f4939578bc429f408c18202573dbafcc4`。这些记录证明版本可解析，不证明安全性、未来可用性或项目已采用。

YAML 的 LICENSE 对文件分别授权：部分从 libyaml 移植的文件为 MIT，其余为 Apache-2.0。应保留两类适用声明，不能简写成“任意选择 MIT 或 Apache”。本项目采用 MIT 并不免除分发依赖时保留版权、许可证及适用 NOTICE 的义务。

## 2. 最小依赖策略

标准库 `flag` 能处理小型命令，Cobra 则减少当前多子命令协议中自建帮助和校验的工作量。现有规划已选择 Cobra，因此建议采用固定版本；无需配套引入 Viper、终端 UI、颜色、DI 或日志框架。配置只有明确列出的字段，环境变量前缀保留不等于读取全部同前缀变量。

YAML 是已确定的人工配置和 Front Matter 格式，标准库不提供解析器；采用维护中的 v3 包，避免自写不完整的 YAML 解析器。JSON 报告与摘要继续使用 encoding/json；Schema 是否采用通用库取决于实际需支持的协议范围，不能靠文件扩展名或样例代替验证。

## 3. 严格输入解析约束

YAML `Decoder.KnownFields(true)` 只约束解码到结构体的未知字段，不构成完整的安全解析策略。实现还需：

- 打开前限制文件类型、路径和大小；读取总量、文档深度、节点数、集合长度和字符串长度均有限额。
- 先校验 Node 树，拒绝 alias/anchor、merge key、自定义 tag、非字符串键、重复键和协议不允许的标量形式；再解码到严格类型。
- 第一次解码后继续检查 EOF，拒绝第二份 YAML 文档及尾部内容；空文档不能默认为有效配置。
- v3 为兼容 YAML 1.1，解码到 bool 时仍接受 yes/no/on/off，整数也保留部分旧格式。若产品只接受 true/false 与规范十进制，应在节点层验证，而非假定库等于完整 YAML 1.2 严格模式。
- JSON 同样需拒绝未知字段、重复键、非预期 null、多份 JSON 与超预算输入；`DisallowUnknownFields` 不能单独检测重复键。

这些约束是应用协议，不是已完成的测试。实施应以恶意输入、重复键、别名、巨大文档和 EOF 正反例证明。

## 4. Schema 离线边界

jsonschema/v6 可以通过 `Compiler.AddResource` 注册内存中的已打包 Schema。固定版本源码的默认 loader 在未注册资源和内置元 Schema都不匹配时返回 `no URLLoader set`，不会自动调用 HTTP。为保持此性质，不接入任意 HTTP/文件 URL loader；只允许明确打包或根内受限读取的资源。

格式断言与 Schema draft 须显式固定。Schema 校验不能替代规则依赖、版本、路径、受管归属、约束冲突和授权校验。即使未引入 Schema 库，`schema_version` 不兼容也必须拒绝写入。

## 5. 根目录与符号链接 API

官方 [Traversal-resistant file APIs](https://go.dev/blog/osroot) 及本机 `go doc os.Root` 确认：Root 方法以打开的目录句柄为边界，拒绝解析后越过根的相对路径；但会跟随根内的符号链接。`os.OpenRoot` 本身也跟随传入根路径中的符号链接。

因此 `OpenRoot` 适合增强产品 I/O 边界，不能直接等同“不跟随链接”：扫描和计划应用仍应通过 Lstat、普通文件判断、输出父目录验证和输入/输出摘要检查拒绝产品协议禁止的链接或替换。Root 不阻止挂载边界、Linux bind mount、`/proc` 或设备文件；平台与文件系统支持必须实测。它也不提供多文件原子事务，仍需日志、备份、互斥与条件恢复。

若要求严格保证不读取任何链接目标，Darwin/Linux 可用 x/sys/unix 按组件打开目录句柄，每级 `Openat` 使用 `O_NOFOLLOW|O_DIRECTORY|O_CLOEXEC`，最终文件使用 `O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC` 并通过 Fstat 校验普通文件。非阻塞打开避免在校验前被 FIFO 挂住；目录枚举结果不能代替读取时的句柄检查。此方案需处理错误、关闭所有句柄、取消/资源限额，并以替换链接/FIFO竞态夹具验证。

本机 Darwin 的标准 syscall 包没有 Openat；为跨 Darwin/Linux 使用同一清晰 API，引入 x/sys/unix 比自行封装裸 syscall 更合适。`Root.OpenFile` 的 O_NOFOLLOW 只约束最终组件，不能单独禁止中间目录链接；SameFile 事后相等检查也不能撤销已发生的内容读取。

Windows 不可复用 Unix flag；应设计拒绝重解析点的句柄流程并原生验证。若暂时只有 os.Root/后验检查，明确阻断尚未安全支持的写入操作，并把严格不读链接的只读保证标为未达成，不能把可交叉编译当作已达成该安全契约。

官方文档指出 JS 平台的 Root 存在 TOCTOU 限制，Plan9/JS 不能在重命名后以句柄追踪原目录；这些平台不应未经额外验证进入支持矩阵。Root 的路径逃逸保护也不能证明传入文件内容可信。

## 6. 未完成核查

依赖的源码审计、完整传递依赖许可清单、SBOM、可达性漏洞扫描、离线构建、平台原生运行与二进制分发均未在本调研中执行。后续使用实际 go.mod/go.sum 运行 `go mod verify`、测试、静态检查与适用供应链门禁，分别保存真实退出结果。不能从“公开版本存在”推导“无风险”。
