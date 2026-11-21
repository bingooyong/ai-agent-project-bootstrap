# 总体设计

沿用docs/02-Architecture、03-Rule-Engine-Design与04-CLI-SDD。单Go module，cmd/ombp薄入口，internal领域和I/O隔离。扫描只读有界，规则解析确定性，写入由计划绑定且可恢复。

Trellis CLI 0.6.17已本地初始化，仅Codex。用户明确授权规划及实现，无需再请求创建/执行确认；每阶段以具体实现和证据验收。
