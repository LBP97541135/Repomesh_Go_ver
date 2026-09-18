# B02.6 UTC 响应修复

真实验收 01 发现，非 UTC 主机上的 pgx 默认 timestamptz 解码使用 time.Local，真实接口返回带 `-07:00` 的 observedAt；前端按已采用的 UTC 契约拒绝。两次登录发起后未完成 GitHub 跳转，现场和失败保留于[真实验收 01](../2026-09-12-b026-live-01/utc-failure.md)。

## 改动

`internal/database/database.go` 的 Open 在每个 pool 连接建立后注册 `TimestamptzCodec{ScanLocation: time.UTC}`。所有数据库来源的认证与发现时间统一表示为 UTC，绝对时间点、数据库值、实际进程时区和期限规则均不变。未放宽前端格式检查。文件里此前已有的 DB.Pool 方法属于原 B02 修改，本次保留。

新增 `internal/database/utc_test.go`：在独立测试子进程设置非 UTC 时区，实际连接两个 PostgreSQL pool 连接，覆盖 binary、text、同一绝对时刻、UTC JSON 和 NULL。测试不修改父进程的 time.Local。

## 新验证

- 修复前 UTC 回归失败，修复后通过；两阶段记录保留。
- 完整构建、Go vet、前端 typecheck/build 与新配套构建通过。
- 首次完整 PostgreSQL 测试使用 peer 认证 socket，旧错误密码用例因环境不支持密码校验而失败。失败记录保留，未据此宣称通过。
- 换用本任务独立 SCRAM PostgreSQL 17 测试库后，完整 `go test -count=1 -json ./...` 退出 0：230 run、229 pass、0 fail、1 skip。唯一跳过项为需要不同文件所有者条件的 TestRootFileOwner，没有 PostgreSQL 用例跳过。
- 独立初审发现测试修改 time.Local 的隔离问题；已改为子进程并完成闭环，当前无开放 P0/P1/P2。
- 本次临时 SCRAM 测试集群已停止，生成的测试数据目录及随机密码文件已移至桌面回收站，可从回收站恢复；测试日志保留。没有清理共享服务或验收数据库。

## 新配套包

`dist/repomesh-0.2.0-b026-utc-20260912-r2/` 已生成，三二进制版本已核实。release.json SHA-256 为 `5af0545707a9feb020436b56dfab407255a6b8d51046eeb4ed7f51bc41c76152`。原 r1 发布包保留。

本批实现与本地回归完成；不代表真实 GitHub 授权或自然刷新通过。下一轮真实操作使用新证据目录，原两次失败不改写为成功。B02 保持 IN_PROGRESS，B03 TODO；未提交或推送。

规划与接口：GPT-6 Astra；实现：gpt-5.6 sol；独立复核：gpt-5.6 terra。
