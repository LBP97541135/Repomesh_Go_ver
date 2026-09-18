# B02.6 第二账号真实验收记录

操作依据为 [真实验收手册](../../current/b02-github-live-acceptance.md)和本目录的 [PLAN.md](PLAN.md)。本文件当前只记录准备状态；所有实际字段必须在操作发生后填写。

## 运行条件

| 项目 | 实际值 |
| --- | --- |
| 开始与结束时间，含时区 | NOT_RUN |
| 操作者与独立复核者 | 账号持有人待执行 GitHub 登录、账号选择、授权同意和邀请接受；独立复核待定 |
| 发布版本 | `repomesh-0.2.0-b026-cookie-20260912-r3`；release.json 哈希与源码清单沿用真实验收 03，执行前重新核对是否变化 |
| 固定 HTTPS origin | `https://repomesh.bohanxu.me:8443` |
| App 与安装 | `repomesh-bohan-b026`；installation 161172403；执行前后重新读取可见性、权限和范围 |
| 专用数据库 | `repomesh_b026`；schema current/target/pending 执行前只读核对 |
| 测试账号 | A `catbobyman`；B `bohanxu111`；邮箱验证由用户于 2026-09-12 10:41 PDT 确认 |
| 仓库样本 | A 持有私仓稳定 ID 1367444901；B 持有且 A 不可见的私仓稳定 ID待实际创建后填写；正文不记录名称 |
| 浏览器 | 等自然刷新观察 02 的 LIVE-10 完成后填写实际 profile、target 与桌面／移动视口 |
| 用户令牌过期与自然刷新 | 依赖自然刷新观察 02 的实际最终结果；当前仍未完成 |
| 替身、时间修改和请求重放 | 必须均未使用 |

## 本轮必需场景

结果只填 PASS、FAIL、BLOCKED 或 NOT_RUN。App 临时公开、邀请发出、第二账号登录成功或等待满 60 秒都不能单独标为 PASS。

| ID | 实际操作和成功判据 | 当前状态 | 实际观察、时间与证据 |
| --- | --- | --- | --- |
| GATE | 自然刷新确认、LIVE-10 浏览器结果、旧会话注销和观察器退出均已核对 | NOT_RUN | 等待刷新观察 02 |
| LIVE-05 | 账号 A 发起重连、GitHub 选择账号 B；attempt 为 ACCOUNT_MISMATCH，A 的 actor/连接/session 不被替换；结果页不显示 B 身份或数据，A 的完整分页结果不包含 B-only 私仓稳定 ID或名称 | NOT_RUN | 待填 |
| LIVE-08-USER-READ | B 初始实际发现 A 私仓 allowed；A 移除 B 读权；保存 allowed、移除和重查三个时间，重查距 allowed 超过 60 秒；B 同一会话重查不再披露旧私仓名称 | NOT_RUN | 待填 |
| RESTORE | 无论成功或失败，App 均恢复 private；A 安装范围和三项权限恢复基线；B 临时安装移除、对 A 私仓的协作者权限已移除 | NOT_RUN | 待填 |
| REVIEW | 独立复核上述证据、状态转换、缓存等待、秘密裁剪与整批结论 | NOT_RUN | 待填 |

## 当前结论

第二账号与邮箱验证准备完成；真实 App 授权、私仓邀请、错误账号重连、读权撤回及恢复均未执行。B02.6 未通过，B02 保持 IN_PROGRESS，B03 保持 TODO。
