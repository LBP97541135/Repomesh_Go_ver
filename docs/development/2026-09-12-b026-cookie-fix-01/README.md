# B02.6 同步回调 Cookie 竞争修复

## 缺陷 B026-CALLBACK-01

优先级 P1。真实同账号重连完成后，数据库创建新会话并撤销旧会话，但仍活跃的浏览器没有收到新 Cookie，随后只能得到 session 401。首次发现时间为 2026-09-12 09:45 PDT。原始失败保留在[数据库快照](../2026-09-12-b026-refresh-01/baseline-reconnect-cookie-failure.txt)和[浏览器事件](../2026-09-12-b026-refresh-01/baseline-reconnect-cookie-failure-events.jsonl)。本次失败不能作为自然刷新基线。

同步 callback 保存交换结果后，将 attempt 发布为 identity_pending。原实现保留了已经到期的 next_run_at，coordinator 可以立即领取身份核实工作。若后台先提交，它按恢复规则不会签发 Cookie；同步 callback 随后只能返回已确认结果，不能补发 Cookie。

## 修复范围

GPT-6 Astra 确定截止时间与后台恢复的接口规则，gpt-5.6 sol 修改 `internal/access/callback.go`，新增 `callback_concurrency_test.go`。CompleteCallback 最长运行 15 秒，并服从更短的调用方期限。发布 identity_pending 的同一条更新将 next_run_at 设为该截止时间，后台在此之前不能领取。

同步路径未返回 Cookie 时，只把相同 binding、attempt ID、identity generation 和 attempt generation 且仍为 identity_pending 的工作释放给后台。若 context 已结束无法释放，保留期限自然到期。confirmed 等终态不变，后台恢复和回调重放仍不能补发 Cookie。没有新 schema 或 Cookie 存储机制。

## 验证状态

[修复前记录](pre-fix.txt)稳定重现 login 和 reconnect 两种竞争。两者均出现后台先 confirmed、同步 callback 无 Cookie；重连同时撤销旧会话。[修复后记录](post-fix.txt)覆盖活跃回调保留、实际 500 毫秒截止后恢复，以及旧回放、注销、同账号与错误账号、跨浏览器保护。普通和 race 定向测试通过。

这些用例使用隔离 PostgreSQL 数据库和测试 provider，不是真实 GitHub 通过证据。[完整验证](validation-summary.json)中 go build、go vet、完整 go test 和前端类型检查均退出 0。Go JSON 记录 230 个测试运行，229 通过、1 项文件所有者条件测试跳过，0 失败；没有 PostgreSQL 用例跳过。使用的新建 PostgreSQL 17.11 SCRAM 测试集群已停止，私有测试数据保留。[独立源码复核](independent-review-01.md)未发现可复现缺陷，真实回归仍待新包运行。

新包 `dist/repomesh-0.2.0-b026-cookie-20260912-r3/` 已构建并验证，三个版本命令及 11 个产物哈希全部匹配。[发布核对](release-r3-verification.json)保存 71 项当前源码清单及原 r1、r2 构建前后哈希，旧包均未变化。r3 的 release.json SHA-256 为 `54363f7b8c1739a44ddc6fc75478fafe057804f200845e057e0fc67b1202c279`。分别匹配源码与发布清单不构成可复现构建或源码到二进制的密码学来源证明。

09:57 PDT 已切换专用 Web 与 coordinator 到 r3。09:58 真实登录和 09:59 同账号重连都收到新 Cookie，随后 session 200、attempt confirmed，旧会话撤销、新会话有效。[真实验收 03](../2026-09-12-b026-live-03/README.md)及其独立复核关闭本缺陷的真实回归缺口。B026-CALLBACK-01 已修复并验证，原失败保留。

[最终保留核对](final-preservation-audit.json)确认旧 batch-02 的 70 个证据文件与 .codex/config.toml 均未变化，未运行旧 verify-record.py。自然刷新已开始等待实际期限，尚未通过。B02 整体 IN_PROGRESS，B03 TODO，修改未提交或推送。

## 执行检查点

1. 完成本地回归和完整检查，独立复核确认无开放 P0/P1/P2。
2. 创建新版本配套包并核对清单。只替换本任务的 Web 与 coordinator，数据库、秘密和已有发布包保留。
3. 在新真实证据目录执行登录和同账号重连，核对 Cookie、session 200、confirmed、旧会话撤销及新会话有效。
4. 真实回归通过后再封存自然刷新基线。缺少第二账号的验收项继续 BLOCKED。
