# B02.6 真实验收 02

本轮使用 UTC 修复后的新 r2 配套包继续真实验收。旧 r1 两次登录发起失败及诊断更正保留在[真实验收 01](../2026-09-12-b026-live-01/README.md)，修复与独立复核见[UTC 修复记录](../2026-09-12-b026-utc-fix-01/README.md)。

## 运行条件

| 项目 | 实际值 |
| --- | --- |
| 开始与结束时间，含时区 | 2026-09-12 07:08 PDT 切换 r2；尚未结束 |
| 操作者与独立复核者 | GPT-6 Astra 编排；gpt-5.6 sol 配置与实现；GitHub 登录、授权由账号持有人操作；本轮完整真实验收复核待执行 |
| 发布版本、release.json 哈希及源码清单 | 0.2.0-b026-utc-20260912-r2；SHA-256=5af0545707a9feb020436b56dfab407255a6b8d51046eeb4ed7f51bc41c76152；当前 70 项源码，UTC 修复目录保存来源范围与保留核对 |
| 固定 HTTPS origin、callback | https://repomesh.bohanxu.me:8443；https://repomesh.bohanxu.me:8443/api/auth/github/callback |
| App ID、Client ID、安装权限及范围 | 用户完成 Install，09:09 PDT 实际设置页为安装 161172403，selected 范围只有专用私仓 ID 1367444901；Metadata 只读、Contents/Pull requests 读写。真实私仓 appCapability=allowed，见 private-discovery-allowed.json |
| 专用数据库代号、schema current/target/pending | repomesh_b026，3/3/0；切换未迁移、未改期限 |
| 测试账号及仓库代号 | 待真实操作；私仓名称仅在受限位置保存 |
| 浏览器版本、桌面与移动视口 | 用户无法看到 WSLg 页面，已改开 Windows Chrome 153.0.8010.36 的专用 profile；固定 GitHub target 和控制方式见 windows-browser.md，实际验收视口待确认 |
| 两个进程 UID、PID、启动时间与停止安排 | UID 1000；Web 192940、coordinator 192941；07:08 PDT 启动，专用验收期间保留 |
| 用户令牌过期已启用、初始 expires_at、refresh_due_at | 首次成功授权实际 has_refresh_token=true；access_expires_at=2026-09-12 16:44:36.846155 PDT，refresh_due_at=16:44:06.846155 PDT，refresh_expires_at=2027-03-12 07:44:36.846155 PST。后续重连尚待执行，这不是最终刷新基线 |
| 替身、时间修改和请求重放 | 真实验收均未使用；UTC 回归中的测试子进程时区仅用于隔离本地测试，不作为真实刷新 |

## 必需场景

| ID | 实际操作和成功判据 | 状态 | 实际观察、时间与证据 |
| --- | --- | --- | --- |
| LIVE-01 | 浏览器信任固定 HTTPS 证书，同源登录入口可用，未登录 session 401；healthz 200、readyz 503 | PASS | 08:26 PDT Windows Chrome 正常显示 RepoMesh 登录按钮；1037×705 视口，同源实际请求 401/200/503。未绕过 TLS。见 real-cancel-events.jsonl 和 windows-browser.md |
| LIVE-02 | 在真实 GitHub 授权页取消，原 attempt cancelled/USER_CANCELLED，无新会话 | PASS | 08:26 PDT 在 GitHub 实际点击 Cancel；callback 303 返回原 d0e866f2-f7e9-4c0b-bda2-0d1028a5fc82 结果页，无 query/hash；API 与只读数据库均为 cancelled/USER_CANCELLED，连接和会话各 0。见 after-real-cancel.txt、real-cancel-events.jsonl |
| LIVE-03 | 真实授权返回固定 callback，303 到结果页，原 attempt confirmed，session 为预期账号；Cookie 标志正确，URL 无 code/state 残留 | PASS | 旧尝试过期原证据保留。08:44 显式新建 0a385caf-1869-411f-a01c-d14556f086d0 后 GitHub 沿用账号持有人已给予的同意自动返回；callback 303、confirmed、session 200 和 connected/idle 均成立。Cookie Secure/HttpOnly/Lax/Path=/；最终 URL 无 query/hash。GitHub 当前账号 catbobyman 的数字 ID 与数据库匹配，显示名 catmem。见 first-confirmed-login*.txt/jsonl 和 first-login-identity.json；独立复查通过，见 independent-login-review-02.md |
| LIVE-04 | 同账号重连原尝试 confirmed，user.id 不变，连接 revision 更新，旧本浏览器会话撤销 | PASS | 08:48 PDT 实际点击新建重连，3ef28404-a5c1-42b6-9041-6344caee1156 confirmed；session user.id 不变，revision 4a517ea9…→a8a723f7…，access_epoch 1→2，旧 generation 1 revoked=true、新 generation 2 有效。见 before/after-same-account-reconnect.txt 和 same-account-reconnect-events.jsonl；该变化不是自然刷新 |
| LIVE-05 | 错误账号重连 ACCOUNT_MISMATCH，原账号和连接不被替换，跨账号仓库结果不泄露 | BLOCKED | 用户明确没有第二个测试账号。不能用当前账号或替身替代，见 second-account-blocker.md |
| LIVE-06 | 已安装专用私仓且用户有读权，userParticipation.status allowed；安装未暂停且三项权限完整，appCapability.status allowed，观察时间可核对 | PASS | 专用私仓 repo_00000000001367444901 已实际返回，userParticipation=allowed（16:09:43Z）、appCapability=allowed（16:10:42Z）；对应安装 161172403 只包含此仓库。私有属性此前由 GitHub 页面确认，见 private-discovery-allowed.json 及其事件 |
| LIVE-07 | 专用 App 权限或安装不足的可读仓库样本，实际 denied 与 reasonCode 对应；恢复配置后新观察 allowed。不能用私仓缺席代替 denied | PASS | 将专用 App 的 Pull requests 从 write 降为 read 后，同一专用私仓仍 userParticipation=allowed，而 appCapability=denied/APP_PERMISSION_MISSING。恢复原请求权限并由账号持有人批准后，09:44 PDT 新查询中同一私仓两项均 allowed。见 permission-reduction-settings.json、private-permission-denied.json、private-permission-restored.json 及其事件；恢复部分独立复核待执行 |
| LIVE-08 | 私仓发现、分页和刷新真实往返，coverage.status partial；安装外私仓缺席不误报完整覆盖，用户失去读权且原观察超过 60 秒后重查，不继续披露旧名称 | BLOCKED | 已发现安装内私仓。浏览器正常 API 请求 limit=5，实际游标三页共 14 项、无重复，末页 partial，见 pagination-01.jsonl；不是页面默认 50 项的“下一页”点击证据。另一个当前用户可读的专用私仓 ID 1367467551 未纳入安装，真实搜索为空且 partial，见 outside-private-fixture.json。撤回用户读权子场景因无第二账号而 BLOCKED，其余实测结果保留 |
| LIVE-09 | 保持原连接直到自然刷新窗口；前后只读快照证明 epoch 增一、revision 与期限前进、connected/idle，期间无登录或重连 | NOT_RUN | 09:45 PDT 建立最终基线的重连暴露 callback 与 coordinator 竞争。数据库 confirmed、新会话 generation 3 已创建，但 callback 无 Set-Cookie、浏览器 session 401。该次不能作为刷新基线，见 ../2026-09-12-b026-refresh-01/baseline-reconnect-cookie-failure.txt 和对应事件。08:48 的旧基线也已被此次重连替代 |
| LIVE-10 | 刷新后同一有效会话的新发现成功，注销后 session 401、清除仓库显示；桌面和移动浏览器真实往返已观察 | NOT_RUN | 待执行 |

## 恢复信息与限制

最新浏览器入口以 [Windows 窗口恢复记录](windows-browser.md)为准：Windows CDP 为 127.0.0.1:9230，使用记录中的固定 GitHub targetId。下列 WSLg 进程和监控属于前一入口，不能用于判断新窗口的登录状态。

- 当前配置：`/home/xubohan/.config/repomesh/auth.json`；运行环境文件在同账号私有目录，正文不入证据。
- 浏览器主 PID 192581，CDP 仅 127.0.0.1:9229；只使用 page 0。浏览器已用 detached 方式恢复，不依赖前一 agent 的交互终端；前次浏览器退出原因未确定。
- 白名单监控 PID 193051，独占输出 `/tmp/repomesh-b02-live/live-monitor-02.jsonl`（0600），不含 query/hash、Cookie 值、csrfToken、授权地址或秘密；不记录 GitHub 登录请求。
- r1→r2 切换脚本首次因 runtime.env 的 export 赋值形式不匹配而安全退出，未停止旧服务。修订后只替换唯一 REPOMESH_RELEASE 行，保留唯一 0600 备份，再校验精确 PID/UID/exe 后替换本任务两个进程；详细结果见 switch.txt。
- B02.1 至 B02.5 的原本地验证证据保留；UTC 缺陷修复已完成本地验证和独立复核。B02.6 完整真实验收未通过，B02 仍 IN_PROGRESS，B03 TODO。未提交或推送。
- 09:45 PDT 的新重连失败不改写 LIVE-04 的早先成功证据，但阻止宣称该能力已经完成真实验收。新缺陷在 ../2026-09-12-b026-cookie-fix-01/ 跟踪。旧会话维持进程于 16:48:13Z 观察到 401 后按规则停止；后续必须先验证新 Cookie 和 session 200，再启动新的维持记录。
