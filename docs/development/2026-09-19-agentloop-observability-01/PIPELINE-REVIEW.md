# 本地证据流水线独立复核

时间：2026-09-19 21:42 UTC。复核范围为 `internal/observepipe/` 的 events、journal、collect、otlp、trials 及 `cmd/repomesh-observe/` 的集成边界；评分规则与平台字段转换的独立复核已完成，见 [EVALUATION-REVIEW](EVALUATION-REVIEW.md)。复核者未修改上述实现，发现问题交由主要实现者修复；本复核新增两个回归测试文件与本报告。

结论：**发现的 4 项问题已修复，针对性回归检查通过；本次复核范围内无待修复项。**这是本地管道与受控错误路径验收，不是 AgentLoop 云评估或 DSH 执行验收。官方本地基础设施结论另见 [LOCAL-REVIEW](LOCAL-REVIEW.md)。

## 问题、修复与复验

| 编号 | 修复前的问题与证据 | 已复核的修复与验收 |
| --- | --- | --- |
| R1／P2 | Go OTLP SDK 对 HTTP 200 的 HTML／未知 Content-Type 返回 nil。独立 CLI 复现中，普通网页服务收到 9 个 Span 请求，工具却报告 `exported=9`、`transport_acknowledged`；再次执行为 `already_acknowledged=9`，不再发送。 | [OTLP 导出器](../../../internal/observepipe/otlp.go)增加响应协议检查，限定 HTTP 200 及受支持的 OTLP Content-Type，再交 SDK 解码；带参数的 MIME 类型先规范化。HTML、缺类型、204、损坏 protobuf／JSON、带 charset 的部分拒收均不能取得成功收据，修正端点响应后按原 Trace／Span 身份重试。 |
| R2／P2 | `import-result` 仅确认 Trial 存在，未检查 trace scope 是否属于该 Trial。独立 CLI 复现：绑定不在本地 `trace_ids` 中的 Trace，仍成功归档 `scored/pass`。 | [CLI 导入](../../../cmd/repomesh-observe/main.go)核对归档 Trial 的 Trace 列表。回归使用另一个真实本地归档 Trial 的 Trace，错误绑定被拒绝且不留平台评分；正确绑定可重复导入但只保留一条。平台 pass 与原确定性 fail 分开显示，原结果不被覆盖。 |
| R3／P2 | `DatasetRows` 没有读取 ManifestRef，也未核对 Trial／Event／Trace 关联。独立 CLI 复现：删除候选 Trial 的 manifest 证据后，dataset 命令仍退出 0、导出 1 行。 | [Trial 归档读取](../../../internal/observepipe/trials.go)增加 manifest 与事件证据校验、Event 数与 Trace 数及身份关联检查。缺失 manifest／report／event，或者引用其他 manifest／Trace／Trial 的事件时均拒绝导出。 |
| R4／P2 | 导出前仅核对 manifest 与 evidence，没有核对独立的 input artifact；已确认收据还会提前跳过全部证据检查。缺少关键输入的本地档案仍可能被报告为全部已确认。 | 导出现在先校验 manifest、evidence 与 input artifact，再复用收据。新增测试覆盖首次发送前及收到成功收据之后删除输入证据，均返回错误，不发送请求、不增加已确认计数。 |

R1—R3 的修复前 CLI 复现全部使用临时目录、临时回环服务与明确标记的固定折扣夹具；没有连接云服务，也没有更改正式 Collector 或业务进程。R4 来自代码路径检查，并补充了独立回归测试。

## 实际验证

复核开始时，已有测试通过：

```bash
go test -timeout=40s -run 'Test(Journal|Collect|OTLP)' ./internal/observepipe
```

已有测试覆盖追加并发、证据摘要、来源身份冲突、未知 schema、跨数据库来源区分、晚提交重扫、OTLP 失败／部分拒收及敏感值不外泄。复核补充的 [integrity_review_test.go](../../../internal/observepipe/integrity_review_test.go) 与 [import_review_test.go](../../../cmd/repomesh-observe/import_review_test.go) 为上述四类遗漏增加行为检查。

修复后执行：

```bash
gofmt -w internal/observepipe/integrity_review_test.go cmd/repomesh-observe/import_review_test.go
go test -timeout=40s -run 'Test(OTLP|DatasetRequires|ImportResult)' ./internal/observepipe ./cmd/repomesh-observe
git diff --check -- internal/observepipe cmd/repomesh-observe
```

两个包均通过，格式检查通过。协议负例还检查返回错误不含端点或响应中的私有标记；已拒收重试使用归档的 Trace／Span ID，成功后重复导出不再次发送。引用错配用例保留合法内容摘要，再制造关联错误，确保测试验证身份关系而不仅是 checksum。

## 结论的边界

本复核确认当前本地归档、来源去重、受控 OTLP 导出、试验证据导出与结果绑定具备所测拒绝行为。它没有独立重跑真实数据库事务测试、完整产品测试、真实云端数据集导入、官方评估器执行或 DSH 运行；这些必须分别引用对应验收记录。原始证据与本地失败判定仍保留，transport acknowledgment 不等于云端已索引或评估通过。
