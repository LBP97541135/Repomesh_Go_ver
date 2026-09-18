# B02.6 真实验收记录模板

本文件仅为模板，所有场景尚未执行。复制到新的运行目录再填写。操作步骤见[真实验收手册](../../current/b02-github-live-acceptance.md)。不得把模板复制、配置检查或本地替身通过标为真实通过。

## 运行条件

| 项目 | 实际值 |
| --- | --- |
| 开始与结束时间，含时区 | 待填 |
| 操作者与独立复核者 | 待填 |
| 发布版本、release.json 哈希及源码清单 | 待填 |
| 固定 HTTPS origin、callback | 待填，仅非秘密地址 |
| App ID、Client ID、安装权限及范围 | 待填，不含秘密 |
| 专用数据库代号、schema current/target/pending | 待填，不含连接串 |
| 测试账号及仓库代号 | 待填，私仓名称证据仅在受限位置保留 |
| 浏览器版本、桌面与移动视口 | 待填 |
| 两个进程 UID、PID、启动时间与停止安排 | 待填 |
| 用户令牌过期已启用、初始 expires_at、refresh_due_at | 待填 |
| 替身、时间修改和请求重放 | 必须均未使用 |

## 必需场景

结果只填 PASS、FAIL、BLOCKED 或 NOT_RUN。每个 PASS 都要填写实际观察和证据路径。没有取消页面、没有权限样本或没有等待到真实刷新窗口均不能填 PASS。

| ID | 实际操作和成功判据 | 初始状态 | 实际观察、时间与证据 |
| --- | --- | --- | --- |
| LIVE-01 | 浏览器信任固定 HTTPS 证书，同源登录入口可用，未登录 session 401；healthz 200、readyz 503 | NOT_RUN | 待填 |
| LIVE-02 | 在真实 GitHub 授权页取消，原 attempt cancelled/USER_CANCELLED，无新会话 | NOT_RUN | 待填 |
| LIVE-03 | 真实授权返回固定 callback，303 到结果页，原 attempt confirmed，session 为预期账号；Cookie 标志正确，URL 无 code/state 残留 | NOT_RUN | 待填 |
| LIVE-04 | 同账号重连原尝试 confirmed，user.id 不变，连接 revision 更新，旧本浏览器会话撤销 | NOT_RUN | 待填 |
| LIVE-05 | 错误账号重连 ACCOUNT_MISMATCH，原账号和连接不被替换，跨账号仓库结果不泄露 | NOT_RUN | 待填 |
| LIVE-06 | 已安装专用私仓且用户有读权，userParticipation.status allowed；安装未暂停且三项权限完整，appCapability.status allowed，观察时间可核对 | NOT_RUN | 待填 |
| LIVE-07 | 专用 App 权限或安装不足的可读仓库样本，实际 denied 与 reasonCode 对应；恢复配置后新观察 allowed。不能用私仓缺席代替 denied | NOT_RUN | 待填 |
| LIVE-08 | 私仓发现、分页和刷新真实往返，coverage.status partial；安装外私仓缺席不误报完整覆盖，用户失去读权且原观察超过 60 秒后重查，不继续披露旧名称 | NOT_RUN | 待填 |
| LIVE-09 | 保持原连接直到自然刷新窗口；前后只读快照证明 epoch 增一、revision 与期限前进、connected/idle，期间无登录或重连 | NOT_RUN | 待填 |
| LIVE-10 | 刷新后同一有效会话的新发现成功，注销后 session 401、清除仓库显示；桌面和移动浏览器真实往返已观察 | NOT_RUN | 待填 |

## 结果与限制

- B02.6 初始为 BLOCKED，等待真实配置。B02 整体仍为 IN_PROGRESS。
- 记录每次失败，修复后在新证据中注明替代关系，不改写旧失败。
- 原始 callback URL、token、Cookie、csrfToken、authorizationUrl、请求正文和数据库秘密不入此记录。
- 独立复核未完成前不得进入 B03。源码有新增修复时，重新验证受影响行为并使用新版本产物，不覆盖 r1。
