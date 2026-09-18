# B02.6 第二账号真实验收 02 记录

当前 **LIVE-05 FAIL / LIVE-08 NOT_RUN / RESTORE_IN_PROGRESS**。依据 [本轮计划](PLAN.md)、[旧准备计划](../2026-09-12-b026-second-account-01/PLAN.md)及[真实验收手册](../../current/b02-github-live-acceptance.md)。

| 范围 | 状态 | 实际证据 |
| --- | --- | --- |
| GATE | PASS | [preflight.json](preflight.json)，刷新最终独立复核PASS；observer/keeper正常收束，旧session401 |
| 样本与临时公开 | OBSERVED | App已临时PUBLIC；B installation `161284386` 仅选B-only稳定ID `1368000734`，邀请及接受证据已保存；恢复尚未完成 |
| LIVE-05 | FAIL | B分页和A基线已确认；实际先完成了A账号reconnect，随后独立B login改变A浏览器上下文，因此错误账号reconnect场景未覆盖，见 [a-live-05-account-mismatch-01.json](a-live-05-account-mismatch-01.json) 与 [a-live-05-failure-timeline.json](a-live-05-failure-timeline.json)。按fail-closed要求未执行事后分页 |
| LIVE-08-USER-READ | NOT_RUN | 同一B会话allowed、移除、>60秒重查及第二次不披露待实际取证 |
| RESTORE | IN_PROGRESS | 两个RepoMesh会话均已logout204/session401；App private、B卸载、协作者/邀请清理、B授权清理、A安装范围与A无B仓库访问待完成或复核 |
| REVIEW | NOT_RUN | 未执行主要操作的独立模型待复核 |

B上下文已记录actor、generation 7及当时的连接状态，B-only ID与A-owned ID的发现结果见 `b-live-05-01.json`。B浏览器原RepoMesh会话和A浏览器误建会话均已注销；B installation ID为 `161284386`。运行结束与恢复独立复核仍待完成。

撤权计时字段：allowedObservedAt、removedAt、recheckedAt、actualElapsedSeconds、secondRecheckedAt均待填。三时间来自实际操作；秒差必须严格大于60。B同会话前后actor/generation及有效标志、无新增B登录/重连、两次响应名称泄露false须分别有证据；不能仅保存结论。

恢复逐项记录：App private、B installation absent、B collaborator absent、pending invitation absent、B authorization absent、A installation161172403 selected仅1367444901、metadata read/contents write/pull requests write、本轮RepoMesh注销及session401。每项状态和实查时间均待填，部分完成不得合并为RESTORE PASS。

失败或阻塞：保存实际步骤、错误码、时间及恢复待办，后续尝试写新记录，不覆盖失败。正文不保存私仓名或认证原始数据。

本轮错误账号reconnect场景未覆盖：时间线证明A账号reconnect先完成，随后另一个独立B login改变A浏览器上下文；现有证据不能把后者归因于错误账号reconnect被接受。本轮保持FAIL并进入必做RESTORE，不继续LIVE-05或LIVE-08。

误建在A浏览器上下文中的B会话与独立B浏览器原session均已通过正常UI注销，随后各自session为401。整轮须先完成 [restore-01.json](restore-01.json) 中全部外部恢复项；先前LIVE-05失败永久保留，本轮不重试。

结论：准备完成不等于真实通过。B02仍IN_PROGRESS，B02.6未VERIFIED；B03 worktree LOCAL_VERIFIED，集成NOT_RUN。
