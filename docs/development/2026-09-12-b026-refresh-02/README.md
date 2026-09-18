# B02.6 自然刷新观察 02

本轮接续同一真实连接。工具修复及[独立复核](independent-tool-review-01.md)已完成，Linux observer PID 293161 于 2026-09-12T17:07:42.514696Z 启动。原 Windows keeper PID 52940 继续保持同一会话。[实际连续采样](continuous-sampling-proof.json)已记录三条，时间分别为 17:07:42.649010Z、17:08:41.338716Z 和 17:09:38.511760Z；连接与 attempt 集合未变化，没有 outcome 或刷新确认文件。当前持续等待自然期限，尚无自然刷新或后续浏览器场景通过结果。见 processes.json。

## 为何另起一轮

[观察 01](../2026-09-12-b026-refresh-01/README.md)在首个样本后因 INVALID_TIMESTAMP 退出。PostgreSQL JSON 会裁去小数秒尾零，Python 3.10 的 fromisoformat 不接受所有合法位数。[后续实际格式核对](../2026-09-12-b026-refresh-01/real-timestamp-format-probe.json)中 100 条真实 clock_timestamp 输出有 11 条被旧解析器拒绝。原失败样本本身未被旧工具记录，这个限制保留。

本轮只修改观察工具。parse_time 使用标准 strptime，支持 0 至 6 位小数秒、Z 及显式时区偏移，拒绝无时区时间。wait_until 每轮只计算一次剩余时间，避免过期边界出现负数 sleep。SQL 和后续浏览器脚本沿用已复核版本，输出路径指向本目录。[红测试](parser-test-red.txt)及[绿测试](parser-test-green.txt)保留；这些固定时间仅验证解析器，不是模拟真实令牌刷新。

## 同一基线与无污染条件

本轮沿用 01 在 2026-09-12T17:00:30.424573Z 封存的原始基线字节，不登录、不重连、不修改任何数据库期限。基线 SHA-256 为 3163816971d13ac908cf44c148dc0e303951b95fc1304b89a2a5cdaa38c989ea。

[启动前连续性核对](continuity-before-start.json)于 17:06:57.422941Z 使用只读事务确认 actor、epoch 5、revision、凭据提交时间、两个 token 期限、自然刷新窗口均未改变；没有新增 attempt，原 session generation 5 仍有效。观察 01 停止到本轮启动间存在采样间隔，不能声称这段时间有连续样本。

| 项目 | 实际值 |
| --- | --- |
| 原真实 reconnect | cbfce23e-631b-4d97-a66a-97b25e0d60bb |
| epoch 与 revision | 5；fb6d2db3-5069-483f-9c79-5efb16b58921 |
| 自然刷新窗口 | 2026-09-12 17:58:45.991534 PDT，即 2026-09-13T00:58:45.991534Z |
| access token 到期 | 2026-09-12 17:59:15.991534 PDT，即 2026-09-13T00:59:15.991534Z |
| 原会话绝对到期 | 2026-09-13T04:59:18.053286Z |
| 会话保持 | 沿用 Windows Node PID 52940，每 10 分钟正常同源 GET；基线仍读 01 的原文件，不启动第二份 keeper |
| 服务与浏览器 | r3 Web PID 287037、coordinator PID 287038；Chrome CDP 127.0.0.1:9230，主 target CBEA86CD7C41D085994D442CEB5B60A4 |

本轮已实际观察三条跨两个采样间隔的记录，首条和第三条都包含旧 Python 解析器不接受的五位小数秒格式。调度按 monotonic 间隔等待，证据时间使用数据库实际 clock_timestamp。进程存活和连续采样均不等于自然刷新通过。

[启动结果独立复核](independent-startup-review-01.md)确认当前连续采样成立。[会话维持后续核对](keeper-continuation.json)还记录 17:10:40Z 的第二次正常 GET 200，以及数据库 generation 5 的 last_active_at 前进，原会话未撤销、epoch 仍为 5。

## 后续取证

观察器每 60 秒读取数据库，窗口前两分钟改为每 10 秒。相同 actor、原会话有效、没有新增 attempt、epoch 恰好增加 1、revision 和凭据提交时间及两个期限前进、提交不早于自然窗口，且连接回到 connected/idle 时，才写 confirmed-refresh.json 并运行 post-refresh.mjs。

后续浏览器脚本在原会话验证新的私仓读取与 App 能力观察、移动视口、实际注销和历史往返。它写 browser-events.jsonl 和 browser-result.json。数据库观察失败则不启动该脚本。所有输出独占创建，不覆盖 01 的失败或本轮结果。

本轮另补足注销后的历史测试准备。刷新及新私仓观察已经成功之后，先通过实际按钮打开 /login，再点击“稍后处理，返回工作区”回到 /，形成可验证的前后历史；不点击任何新授权提交按钮，并检测意外 login/reconnect POST 后失败。随后实际注销，再做 back 和 forward。这样不会依赖旧 reconnect 结果页在 401 后改写历史的行为。该补充只改变后续浏览器工具，不改变产品或当前等待中的连接。

[后续浏览器工具复核](independent-tool-review-02.md)已通过；脚本尚未执行，不能据此标记 LIVE-10 通过。

保持电脑、WSL 专用服务和专用 Chrome 运行，观察期间不登录、重连、手动注销或改变 App 授权。必要时可在本目录新建 STOP 文件结束 observer；不编辑基线或旧结果来重试。

真实结果生成后还须独立复核。账号持有人已创建第二账号 `bohanxu111`，错误账号重连与撤回用户读权从缺少前置账号改为 READY_PENDING。为保护本轮基线，这些操作只在自然刷新和后续浏览器检查结束后开始。本轮即使通过，也要等两项真实结果及整批独立复核完成才能将 B02.6 标记通过。当前 B02 IN_PROGRESS，B03 TODO。
