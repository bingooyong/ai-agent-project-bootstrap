# 实施步骤

1. 复核依赖证据和当前工作区；读取PRD、设计、上下文规范。
2. 细化接口和验收用例；独立审查后进入实现。
3. 实现最小完整行为并添加有意义的正反、边界与安全回归。
4. 执行gofmt、go vet、go test -race，按需要构建/E2E和平台验证。
5. 独立审查、修复、记录证据与缺口，更新DAG。未通过不归档。

## 首批验证样本

Go go.mod、Python pyproject/requirements、React package.json及TS配置、混合嵌套module、空目录；失效/重复键/深层输入、脚本副作用诱饵、symlink/FIFO/非普通文件、遍历和读取预算、依赖环、P0同ID替换、显式冲突。scanner无执行输出，缺覆盖返回error并标incomplete；rules未知条件拒绝，不猜测自然语言一致性。
