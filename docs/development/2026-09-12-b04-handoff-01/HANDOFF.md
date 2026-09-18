# B04 下一会话准备交接

状态：`DESIGN_PREPARED_NOT_ADOPTED`。本次仅新增交接和 Prompt；没有实施 B04、修改产品、运行测试或采用候选。准备角色为 GPT-6 Astra。

下一会话使用 [完整 Prompt](NEXT-SESSION-PROMPT.md)，当前入口另见 [NEXT-TASK-B04-PROMPT](../../archive/2026-09-13-plan-organization/NEXT-TASK-B04-PROMPT.md)。两份 Prompt 本次以同一正文生成；后续如改动须同时核对。

## 当前证据与状态

主目录 `/home/xubohan/projects/Repomesh_Go_ver` 已完成 B03 文件集成。已读取 integration-01 的旧 README/checks 与末轮实际结果，旧 README 仍写验证 pending，因此不能只据它判断最终状态。已核到：

- `backend-verification-20260913-062605/checks.json` 为 BACKEND_LOCAL_VERIFIED，failure=null、cleanupPassed=true，40 个检查，记录的命令退出码均符合各自预期。
- `frontend-verification-20260913-062547/` 为本轮前端末次检查入口。
- `browser-normal-20260913-062417/browser-result.json` 与 `browser-empty-20260913-062531/browser-result.json` 均 passed=true。
- `release-verification-20260913-062726/` 与 `release-validation-20260913-062934/summary.json` 对应新包 `dist/repomesh-0.3.0-b03-integrated-20260912-r1/`；summary 为 artifactCheck passed、三个版本一致、迁移 4/4/0、实际进程恢复 passed=1/skipped=0、businessReady=false。

最终独立复核在本交接编制时进行中；下一会话必须读取 integration-01 实际最终复核/汇总及其最终源码绑定。只有明确 PASS 且无开放 P0/P1/P2 才按 INTEGRATED_LOCAL_VERIFIED 接手，不预先把 B03 或 B02 标 VERIFIED。若文件缺失/待审/失败，先处理具体缺口。不要把旧 worktree LOCAL_VERIFIED 自动当作主目录验收。

B02 账号验证 PAUSED_BY_USER，整体 IN_PROGRESS。历史 LIVE-05 FAIL、LIVE-08 NOT_RUN、RESTORE_IN_PROGRESS 保留，未来跨账号项 DEFERRED_BY_USER；外部恢复责任没有被文档抹掉，但本次不执行账号或清理操作。以后真实账号验证只用 A；本地合成 actor 隔离回归仍必需。

## B04 候选位置和待收口差异

候选保留在 `/home/xubohan/projects/Repomesh_B03/docs/development/2026-09-12-b04-design-01/`，未复制旧证据：

1. `backend-candidate-a.md` 与 `backend-interfaces-a.go.txt`：业务持事务、秘密准备/插入分离、Provider/modelProfile 不可变映射、持久 save/close 与执行导入。
2. `http-ui-candidate-b.md` 与 `frontend-interfaces-b.ts.txt`：唯一模型 HTTP 稿六端点、Key 发出后恢复、页面/身份隔离、空目录与默认 UX。
3. 已发现 `backend-interfaces-final.go.txt` 和 `frontend-interfaces-final.ts.txt`，头部仍 PROPOSED_NOT_ADOPTED，是中断综合草稿，不是已采用 final。

中断草稿取消独立 comparison-key/MAC，改为 protected input vault 比较；将 import 单例锁提前到 owner 锁之前；增加服务端 model-save Destination resolver；统一 displayName 输出字符串及 fieldErrors 必返数组。下一 Astra 应完整审查与源码兼容性、锁序和秘密目的权限后明确采用，不能把文件名 final 当审批。A 的执行导入裁减与默认 pinned_version 是对来源草案的明确替代提案，B 没有另造 import schema，两者需统一进唯一来源稿。

B04 只模型/执行配置来源、不可变版本、模型秘密保存与安全终结。B05+ 的测试/应用/费用/预算、Issue/P9/运行/AgentTeams 不进入。规划/架构/全部必要接口 Astra，Sol 实现，独立其他非主要实现模型复核；禁止先填 helper 再补签名并声称遵守前置流程。

## 本次检查与限制

只读文件/结构和 JSON 白名单摘要核对；未启动服务或浏览器，未执行工程/历史测试。仅新增本目录两文件和 current Prompt；根 HANDOFF/IMPLEMENTATION-PLAN 留给最终复核后的统一状态更新。本交接与 Prompt 的最终状态不依赖旧文档时间戳或未知 PID。
