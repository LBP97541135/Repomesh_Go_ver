# 下一任务 Prompt：B04 精确采用与实施接续

将下列正文复制给下一任务。默认从采用审查开始；只有该任务获得明确采用与实施授权后，才进入 B04 实现。B05／B06 的设计完成不构成其实施授权。

---

请使用 GPT-6 Astra，reasoning effort=xhigh，接手 RepoMesh 的 B04。工作目录为 `/home/xubohan/projects/Repomesh_Go_ver`。你负责规划、架构、核心类型、事务及函数声明。B04—B06 前置设计已完成且独立设计复核 PASS，请从已交付设计接续，不重新从历史候选开始综合。

先核对 Linux、实际 cwd、Git 分支／HEAD／完整含未跟踪文件的状态，建立本任务非秘密基线。保留全部既有改动、历史证据、旧发布包与另一 B03 worktree；同步记录中的旧 HEAD 不是当前源码版本。不要读取真实 auth config、包装根、token／Key／数据库密码正文或进程环境。

完整阅读：

1. 根 AGENTS.md、README.md；docs/current/README.md、HANDOFF.md、DEVELOPMENT-START.md、IMPLEMENTATION-PLAN.md、ASTRA-DESIGN-PREPARATION.md。
2. docs/development/2026-09-13-b04-b06-handoff-01/HANDOFF.md 与 SYNC.md。
3. docs/development/2026-09-13-b04-b06-design-01/ 的 README.md、PLAN.md、DECISIONS.md、B04.md、B05.md、B06.md、b04.go.txt、b05.go.txt、b06.go.txt、frontend.ts.txt、migration-design.md、ACCEPTANCE.md、COMPATIBILITY.md、SOURCE-AUDIT.md、REVIEW.md、checks.json；通过清单核对材料与真实源码的关系。
4. B04 设计指向的唯一来源、模型内部操作、模型浏览器、Key 保存及恢复、项目配置与原型专题，结合 B02 采用范围和 B03 项目契约逐节判定采用状态。B05／B06 中与 B04 共享的秘密、默认绑定、固定版本、执行材料完整性和 P9 依赖必须核对。
5. docs/development/2026-09-12-b03-integration-01/README.md、FINAL-INDEPENDENT-REVIEW.md 及其实际末轮结果；真实 internal/secrets、access、projects、github、jsoninput、database、web、testdb、三个入口、发布／验证脚本与前端 API、会话、路由、配置／原操作恢复源码。

当前事实：B02 IN_PROGRESS／外部 PAUSED_BY_USER；保留历史 LIVE-05 FAIL、LIVE-08 NOT_RUN、RESTORE_IN_PROGRESS，未来跨账号项 DEFERRED_BY_USER。不恢复账号实验，不索取第二账号。B03 INTEGRATED_LOCAL_VERIFIED，businessReady=false。B04 DESIGN_PREPARED_NOT_ADOPTED，B05／B06 实施 TODO；完整 G1／G2 未完成。09-12 的 Prompt、历史候选及旧阶段角色指令仅是来源，不自动授权新工作。

先交付一份可直接审阅的 B04 精确采用记录草案，包含 D01—D04、六个 HTTP 端点、Key UI／原 save 恢复／close、来源 schema 1 与默认 pinned_version、锁序、迁移、兼容要求以及对旧章节的逐项替代。区分已采用规则和新推荐。如果当前任务没有收到对该范围的明确采用与实施授权，就在完成草案、接口核对和独立设计复核后提出一次集中的采用问题；等待期间可继续独立准备，不写依赖未采用选择的产品实现。若授权已经给出且范围明确，记录原话与范围后直接继续，不重复索取批准。不顺带采用 D05—D08／S06。

获准后仅实施 B04：

- U04.1 秘密 Prepare／InsertPrepared 和认证回归。先在业务锁外独立预扣包装，再加入业务事务；预扣不退款；原认证 API 保留。
- U04.2 Provider、不可变模型版本、目录、save／close。独立 purpose／owner 的完整输入 vault 做精确比较，不保存裸 Key 哈希；业务密文／模型／目录／head／原回执同事务。旧结果先于新修订资格，原 saveId 冲突不覆盖赢家；close 与 save 争同一槽，空槽持久关闭阻挡迟到请求，成功不能被 close 撤销。
- U04.3 受控数据库部署身份的执行来源 CLI。schema 1 每项单 owner，导入单例先于排序 owner 锁；新默认必须 pinned_version，旧 null 保持 B03；版本和默认与回执原子提交，原 importId 稳定重放。没有预算／时限的旧版本不能伪装成 B06 可用配置。
- U04.4 六个模型 HTTP 端点和浏览器恢复。字段以采用后的唯一契约为准；Key 发出即清内存，不入存储／日志／URL／HAR。未知状态保留原键并查询或明确 close；401／换 actor 清敏感派生状态，不能取消的晚响应也受 generation 限制。模型页面遵守已采用原型布局与使用流程。

获准实施时使用明确路由为 GPT-5.6 Sol 的实现子代理，按文件唯一归属分工；Astra 先给每个单元的公共及必要私有声明，再由 Sol 填函数体。新增 helper 或接口变化先提交具体签名由 Astra 确认。独立复核由未承担主要实现者完成。工具无法提供指定模型时明确说明，不以文本角色名假装完成模型路由。

B04 不发送真实或模拟成功的模型业务请求，不实现测试／费用／预算账本、专用模型应用、Issue／P9、Manager／Worker、MCP、AgentTeams 适配或 B07—B11。不把管理可用性写成运行 Ready。B05／B06 的声明只用于兼容核对，不预铺未授权目录和框架。

每个实施单元以本轮 ACCEPTANCE.md 的对应映射及已采用的 B04 验收矩阵验证。至少覆盖真实 PostgreSQL 同键同输入 20 并发、异输入冲突、故障回滚、save／close 竞争、提交丢响应与重启恢复、秘密用途和根状态、导入幂等与固定默认、B03 回归、六端点契约、浏览器存储禁用／换 actor／真实晚响应。真实数据库用例 skip 不计为 PASS。

代码变更后执行 go build ./...、go test ./...、go vet ./...、适当 race，以及 npm --prefix web ci、typecheck、test、build；按改动验收隔离 PostgreSQL、实际浏览器与唯一新版本配套发布。资源必须本任务新建且拥有，不触碰 B02 专库／55432／r3／既有浏览器 profile／旧包，不重跑旧实验脚本。失败轮次保留；修复后只重跑受影响验证及必需完整检查。

最终独立复核实际最终源码及证据，关闭所有 P0／P1／P2；更新使用说明、当前交接、计划和精确采用记录，分别报告 DESIGN_ADOPTED、IMPLEMENTED、LOCAL_VERIFIED、外部 NOT_RUN。不得将 B02 暂停或 B04 本地完成改成整批 VERIFIED。记录实际模型分工及流程偏差、命令退出码／通过数／跳过、产物路径和剩余限制。完成 B04 后停止，不自动实施后续批次或部署；本 Prompt 不另行授予后续提交／推送权限，按下一任务实际用户授权处理 Git。
