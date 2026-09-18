# B02.6 自然刷新观察 01

当前状态为 FAILED_OBSERVER。观察器在首个样本后因 INVALID_TIMESTAMP 退出，outcome.json 保存失败，不能称为持续观察或自然刷新成功。浏览器会话维持进程仍有效。新一轮会在独立目录继续使用同一真实连接；本目录基线、失败输出及旧脚本全部保留。

## 基线与实际期限

最初在 r2 上建立基线失败，数据库 confirmed 但浏览器没有得到 Cookie。原失败保留为 baseline-reconnect-cookie-failure.txt 和对应事件。随后完成[Cookie 竞争修复](../2026-09-12-b026-cookie-fix-01/README.md)，在[真实验收 03](../2026-09-12-b026-live-03/README.md)中验证 r3 的登录及同账号重连，并通过独立复核。

baseline.json 于 2026-09-12T17:00:30.424573Z 从只读事务建立。对应实际 reconnect cbfce23e-631b-4d97-a66a-97b25e0d60bb，actor 16198e14-7249-4cc2-a15e-57cd8b568874，epoch 5，revision fb6d2db3-5069-483f-9c79-5efb16b58921，session generation 5。此前的连接版本和期限均不能代替此基线。

| 条件 | 实际时间 |
| --- | --- |
| 自然刷新窗口开始 | 2026-09-12 17:58:45.991534 PDT，即 2026-09-13T00:58:45.991534Z |
| access token 到期 | 2026-09-12 17:59:15.991534 PDT，即 2026-09-13T00:59:15.991534Z |
| refresh token 到期 | 2027-03-12T16:59:15.991534Z |
| 会话绝对到期 | 2026-09-12 21:59:18.053286 PDT，即 2026-09-13T04:59:18.053286Z |
| 观察失败截止 | 初始 access token 到期后 15 分钟 |

这些期限来自真实 GitHub 授权及只读数据库快照。没有修改数据库期限、模拟时钟、手动重放 token，或以新登录代替刷新。

## 持续进程

- Linux observer 原 PID 292515，已因 INVALID_TIMESTAMP 退出。observations.jsonl 仅有首个样本，outcome.json 为 FAILED。
- Windows keeper PID 52940，执行 /tmp/repomesh-b02-live/session-keepalive-02.mjs，每 10 分钟在固定 Chrome target 正常 GET /api/session。输出 /tmp/repomesh-b02-live/session-keepalive-02.jsonl。脚本副本保存在本目录。
- Windows Chrome 固定 target CBEA86CD7C41D085994D442CEB5B60A4，CDP 127.0.0.1:9230。页面已停留在工作区。保持这台电脑和专用浏览器运行，观察期间不登录、不重连、不手动注销或调整 App 权限。
- 配套 r3 Web PID 287037、coordinator PID 287038，专用 PostgreSQL 55432 保持运行。没有停止或清理其他服务。

keeper 首次实际返回 200。keeper-startup-proof.json 证明 generation 5 的 last_active_at 从 17:00:20.609977Z 前进到 17:00:40.749846Z，连接版本和尝试集合均未变化。观察器首个样本为 17:01:09.449187Z，仍为 epoch 5、connected/idle。进程启动记录见 processes.json；进程存活不等于刷新完成。

## 完成判据与失败处理

observer 只允许相同 actor、epoch 恰好增加 1、revision 与凭据提交时间和两个期限前进，并且提交时间不早于自然刷新窗口。必须回到 connected/idle，原会话仍有效，期间没有新增登录或重连尝试。同 binding 的匿名 pending login 也会被检测。

只有满足全部刷新判据，才写 confirmed-refresh.json 并启动 post-refresh.mjs。该脚本在原会话中查询专用私仓，要求用户与 App 两项新观察晚于刷新提交；随后验证移动视口、实际注销返回 204、session 401，以及真实前进后退不显示旧仓库。各阶段结果独占保存为 browser-events.jsonl 和 browser-result.json。脚本不会自动确认 GitHub 授权，也不会重登录或补发 Cookie。

出现未知状态、会话失效、新尝试、异常版本变化、数据库不可用或超出等待期限时，观察写失败并停止。keeper 遇到非 200、错误 origin、网络或解析错误、账号不匹配也会非零退出。必要时可在本目录新建 STOP 文件让 observer 安全结束；不编辑基线或旧输出来重试，新一轮须另建证据目录。

工具的独立复核见 independent-observer-review-02.md 和 independent-observer-review-03.md。02 中的 keeper 失败停止问题已修正，旧审查记录保留。Astra 负责规划与接口，Sol 实现脚本，Terra 独立复核。真实结果生成后仍须独立审阅，不能仅凭脚本返回值将整批标记 VERIFIED。

[启动结果独立复核](independent-startup-review-01.md)确认本轮实际观察失败。进一步[只读时间格式核对](real-timestamp-format-probe.json)取得 100 个真实 PostgreSQL clock_timestamp JSON 值，其中 11 个被 Python 3.10 的 fromisoformat 拒绝，示例含 5 位小数秒。PostgreSQL JSON 会裁去小数尾零，旧工具只支持的部分小数位格式不足。原来触发退出的那一个样本没有被旧工具记录，因此格式核对是后续复现，不能伪称原始失败样本。此问题限于观察工具，不修改产品时间或令牌期限。

缺少第二账号的错误账号重连和用户读权撤回两项仍 BLOCKED。即使本目录的自然刷新及后续浏览器场景全部通过，B02.6 也不能整批通过，B03 继续 TODO。
