# B02.6 真实验收 01

用户在 GitHub App 本机导入操作后回复“OK了”。本轮从实际导入结果继续，采用既有真实验收模板。配置完成、服务启动与完整真实验收分别记录。

## 运行条件

| 项目 | 实际值 |
| --- | --- |
| 开始与结束时间，含时区 | 2026-09-12 06:50 PDT 开始；尚未结束 |
| 操作者与独立复核者 | GPT-6 Astra 编排；gpt-5.6 sol 配置与启动；账号持有人完成 GitHub 登录和授权；本轮独立复核待执行 |
| 发布版本、release.json 哈希及源码清单 | repomesh-0.2.0-b02-local-20260912-r1；release.json SHA-256 为 eb421e6d7c901aad20dbfce071b685433b1aa9541c3b661be700dd320df4cdb4；69 项已记录源码和 11 项发布文件哈希匹配 |
| 固定 HTTPS origin、callback | https://repomesh.bohanxu.me:8443；https://repomesh.bohanxu.me:8443/api/auth/github/callback |
| App ID、Client ID、安装权限及范围 | 本机已导入；权限和安装范围待真实核查，未将配置存在视为 GitHub 生效 |
| 专用数据库代号、schema current/target/pending | repomesh_b026；本轮启动检查为 3/3/0，见 startup.md |
| 测试账号及仓库代号 | 待实际操作；私仓名称仅在受限位置保留 |
| 浏览器版本、桌面与移动视口 | Windows 桌面上的 WSLg 有头 Chromium 153.0.8010.12，专用独立 profile，1050×737；移动视口待执行 |
| 两个进程 UID、PID、启动时间与停止安排 | UID 1000；Web 177022、coordinator 177023，06:51 PDT 启动；自然刷新期间保留专用服务 |
| 用户令牌过期已启用、初始 expires_at、refresh_due_at | 待 GitHub 设置与首次真实授权确认 |
| 替身、时间修改和请求重放 | 均未使用 |

## 必需场景

| ID | 实际操作和成功判据 | 状态 | 实际观察、时间与证据 |
| --- | --- | --- | --- |
| LIVE-01 | 浏览器信任固定 HTTPS 证书，同源登录入口可用，未登录 session 401；healthz 200、readyz 503 | PASS | startup.md、preflight.md 与 browser-initial.md；真实有头浏览器正常 TLS 页面返回 200 并呈现登录按钮 |
| LIVE-02 | 在真实 GitHub 授权页取消，原 attempt cancelled/USER_CANCELLED，无新会话 | BLOCKED | utc-failure.md：尚未到达 GitHub 取消步骤，等待修复实际 UTC 时间输出缺陷 |
| LIVE-03 | 真实授权返回固定 callback，303 到结果页，原 attempt confirmed，session 为预期账号；Cookie 标志正确，URL 无 code/state 残留 | BLOCKED | navigation-01.md 与 utc-failure.md：两次 start 201 后未完成跳转，尚无会话或连接 |
| LIVE-04 | 同账号重连原尝试 confirmed，user.id 不变，连接 revision 更新，旧本浏览器会话撤销 | NOT_RUN | 待执行 |
| LIVE-05 | 错误账号重连 ACCOUNT_MISMATCH，原账号和连接不被替换，跨账号仓库结果不泄露 | NOT_RUN | 待执行 |
| LIVE-06 | 已安装专用私仓且用户有读权，userParticipation.status allowed；安装未暂停且三项权限完整，appCapability.status allowed，观察时间可核对 | NOT_RUN | 待执行 |
| LIVE-07 | 专用 App 权限或安装不足的可读仓库样本，实际 denied 与 reasonCode 对应；恢复配置后新观察 allowed。不能用私仓缺席代替 denied | NOT_RUN | 待执行 |
| LIVE-08 | 私仓发现、分页和刷新真实往返，coverage.status partial；安装外私仓缺席不误报完整覆盖，用户失去读权且原观察超过 60 秒后重查，不继续披露旧名称 | NOT_RUN | 待执行 |
| LIVE-09 | 保持原连接直到自然刷新窗口；前后只读快照证明 epoch 增一、revision 与期限前进、connected/idle，期间无登录或重连 | NOT_RUN | 尚未有真实连接；不开始约 8 小时等待计时 |
| LIVE-10 | 刷新后同一有效会话的新发现成功，注销后 session 401、清除仓库显示；桌面和移动浏览器真实往返已观察 | NOT_RUN | 待执行 |

## 结果与限制

- B02.1 至 B02.5 保持 LOCAL_VERIFIED；B02.6 尚未通过，B02 整体 IN_PROGRESS，B03 TODO。
- 源码和发布文件分别匹配清单，不代表可复现构建或源码到二进制的密码学来源证明；本轮未重跑历史产品验收。
- 接手核对首次尝试使用 jq，但工具未安装，清单验证未执行；随后使用 Python 标准库读取 JSON 并核对成功。没有安装依赖或改写历史清单。
- 本机导入后的 client secret 和 PEM 都是当前用户所有的普通 0600 文件。没有把正文、摘要或认证令牌写入证据。
- 原始 callback URL、token、Cookie、csrfToken、authorizationUrl、请求正文和数据库秘密不入本目录。每次失败保留，不覆盖历史证据或 r1 发布包。
- 完整必需场景及独立复核结束前，不进入 B03。
