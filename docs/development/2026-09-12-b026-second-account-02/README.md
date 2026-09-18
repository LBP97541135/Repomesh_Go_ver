# B02.6 第二账号真实验收 02

状态 **IN_PROGRESS / LIVE-05 FAIL / RESTORE_IN_PROGRESS**。B账号初始分页已观察到两项目标能力均为allowed；时间线表明A账号reconnect先完成，12秒后独立B login改变A浏览器上下文，因此计划要求的错误账号reconnect场景未覆盖，不能称为 `ACCOUNT_MISMATCH` 产品缺陷。失败证据见 [a-live-05-account-mismatch-01.json](a-live-05-account-mismatch-01.json) 与 [a-live-05-failure-timeline.json](a-live-05-failure-timeline.json)。LIVE-08 NOT_RUN，B02仍未通过。冷启动续接见 [PAUSE.md](PAUSE.md)。

会话恢复见 [a-accidental-b-session-cleanup-01.json](a-accidental-b-session-cleanup-01.json) 与 [restore-01.json](restore-01.json)：A浏览器中的误建B会话和独立B浏览器原session均已注销。本轮不再重试；须先完成App、安装、协作者、授权及A访问范围的全部外部恢复核验，先前LIVE-05失败不覆盖、不改写。

[刷新最终独立复核](../2026-09-12-b026-refresh-02/independent-final-review-01.md)明确判定 LIVE-09、LIVE-10 PASS，无开放 P0/P1/P2；这些项目不重跑。结果和哈希保存在 [preflight.json](preflight.json)。

本目录接续并保留 [准备轮次 01](../2026-09-12-b026-second-account-01/README.md)，不覆盖其历史 NOT_RUN。A浏览器中的误建B会话与B浏览器原RepoMesh会话现均已正常logout 204，后续session 401；本轮不再执行LIVE-08。

执行见 [PLAN.md](PLAN.md)，逐项结果填入 [live-acceptance.md](live-acceptance.md)。所有 GitHub 登录、账号选择、授权同意、安装确认、邀请接受及必要审批由账号持有人完成。正文只记稳定 ID、状态、布尔值和时间，禁止记录私仓名称、秘密及认证原始数据。成功、失败或阻塞均须完成恢复，恢复尚未完成就不能交付为通过。

当前App仍为PUBLIC；因B installation `161284386` 仍存在，GitHub拒绝Make private。恢复必须先卸载该B installation，再由A将App改回private。其余顺序及外部现状见 [PAUSE.md](PAUSE.md)。
