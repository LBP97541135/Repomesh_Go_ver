# B02.6 真实验收 03

本轮验证 r3 对同步 callback Cookie 竞争的修复，并为当前账号建立自然刷新基线。旧 r1、r2 的失败和成功记录全部保留。[本地修复与验证](../2026-09-12-b026-cookie-fix-01/README.md)和[真实验收 02](../2026-09-12-b026-live-02/README.md)分别记录对应范围。账号持有人已完成的 GitHub 登录及授权同意继续有效；本轮显式发起登录和重连后，GitHub 自动返回，代理没有代替持有人点击授权同意。

## 运行条件

| 项目 | 实际值 |
| --- | --- |
| 开始时间 | 2026-09-12 09:57:37 PDT 切换；本轮尚未结束 |
| 发布版本 | 0.2.0-b026-cookie-20260912-r3，Linux AMD64 |
| release.json SHA-256 | 54363f7b8c1739a44ddc6fc75478fafe057804f200845e057e0fc67b1202c279 |
| 来源范围 | 71 项当前源码清单及 11 项发布产物匹配，见修复目录 release-r3-verification.json；不构成可复现构建或密码学来源证明 |
| Web 与 coordinator | UID 1000；Web PID 287037，coordinator PID 287038；独立日志路径及环境备份见 switch.json |
| 固定 origin 与 callback | https://repomesh.bohanxu.me:8443；/api/auth/github/callback |
| 认证配置 | /home/xubohan/.config/repomesh/auth.json；秘密正文未入证据 |
| 专用数据库 | repomesh_b026；Unix socket 55432；schema 3/3/0，切换未迁移 |
| 浏览器 | Windows Chrome 专用 profile；CDP 127.0.0.1:9230；主 target CBEA86CD7C41D085994D442CEB5B60A4 |
| 账号与 App | 沿用真实验收 02 的已核验账号、App 及 selected-repositories 安装 161172403 |
| 最终基线 | reconnect cbfce23e-631b-4d97-a66a-97b25e0d60bb；epoch 5；revision fb6d2db3-5069-483f-9c79-5efb16b58921；session generation 5 |
| 自然期限 | access_expires_at 2026-09-12 17:59:15.991534 PDT；refresh_due_at 17:58:45.991534 PDT；refresh_expires_at 2027-03-12 08:59:15.991534 PST |
| 会话绝对期限 | 2026-09-12 21:59:18.053286 PDT；每 10 分钟同源 GET 保持活跃，空闲期限仍为 30 分钟 |
| 替身、时间修改和 token 重放 | 本轮真实操作均未使用 |

## 逐项结果

| ID | 状态 | 本轮实际结果与证据 |
| --- | --- | --- |
| LIVE-01 | PASS | r3 切换后 Windows 浏览器正常校验证书，healthz 200、readyz 503、未登录 session 401。随后登录及结果页同源返回，见 login-events.jsonl |
| LIVE-02 | NOT_REPEATED | r2 的真实取消已通过，见真实验收 02；本轮未重跑，不把旧结果冒充 r3 实测 |
| LIVE-03 | PASS | 09:58 PDT login 3fe25d1c-de6f-4996-98e5-cd3304038582 confirmed，callback 303 带新 Cookie，session 200，同一 actor，generation 3 撤销、4 有效。见 after-login.txt、login-events.jsonl |
| LIVE-04 | PASS | 09:59 PDT reconnect cbfce23e-631b-4d97-a66a-97b25e0d60bb confirmed，callback 303 带新 Cookie，session 200，同一 actor，epoch 4 增至 5，generation 4 撤销、5 有效。见 after-reconnect.txt、reconnect-events.jsonl。独立复核见 independent-cookie-regression-review-01.md |
| LIVE-05 | READY_PENDING | 10:37 PDT 用户报告已创建第二账号 bohanxu111，公开资料确认账号存在；10:41 PDT 用户确认邮箱验证完成。为保护正在运行的自然刷新基线，尚未加入专用 Chrome 或执行错误账号重连 |
| LIVE-06 | NOT_REPEATED | r2 已确认安装内专用私仓的用户与 App 两项资格 allowed，见真实验收 02 |
| LIVE-07 | NOT_REPEATED | r2 已完成同一样本 APP_PERMISSION_MISSING 到恢复 allowed，独立复核通过，见真实验收 02 |
| LIVE-08 | READY_PENDING | r2 的私仓发现、三页 API 游标和安装外私仓缺席证据保留。第二账号已存在；私仓邀请、初次 allowed、撤销协作者、等待超过 60 秒及不再披露名称均待自然刷新和 LIVE-10 完成后执行 |
| LIVE-09 | IN_PROGRESS | 已封存实际基线并启动只读观察，尚未到刷新窗口。见刷新观察 01；epoch 5 来自重连，不能计作自然刷新 |
| LIVE-10 | NOT_RUN | 刷新后发现、移动视口、注销及浏览器往返脚本已准备和审阅，只有真实刷新确认后才自动执行 |

已返回工作区，同源 URL 没有 query/hash，session 200，见 workspace-session.json。连接观察超过 60 秒时 API 返回 unknown 是当前保守展示规则；只读基线中的连接仍为 connected/idle。

## 接续

[观察 01](../2026-09-12-b026-refresh-01/README.md)保存原基线、会话维持证明及时间解析失败。工具修复后，[自然刷新观察 02](../2026-09-12-b026-refresh-02/README.md)沿用相同基线字节及真实连接继续取证，采样间隔与只读连续性核对均有记录，没有重连。真实 Cookie 缺陷已完成本地修复、独立源码复核与新包真实回归；B02.6 整批未通过，B02 IN_PROGRESS，B03 TODO。未提交或推送。
