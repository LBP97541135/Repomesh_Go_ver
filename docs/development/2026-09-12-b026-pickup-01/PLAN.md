# B02.6 接手核对与后续门槛

- [x] Read the Principles section of the poteto-mode skill.
- [x] Phase A: Frame
- [x] Phase B: Design the workflow
- [ ] Phase C: Run the loop
- [x] 核对 Linux、工作目录、Git 状态和原服务实际版本。
- [x] 完整阅读用户指定文档，核对原观察器、keeper 和全部已生成结果。
- [ ] 等待原自然窗口，以同一 actor、原 session、无新 attempt 和准确单次凭据推进证明 LIVE-09。
- [ ] 核对原观察器生成的 LIVE-10 真实浏览器结果，并独立复核。
- [ ] 在新证据目录执行第二账号计划，记录恢复前后状态，再独立复核全部必需场景。
- [ ] 仅当全部必需场景通过且无开放 P0/P1/P2 时，将 B02 标为 VERIFIED。
- [ ] B02 通过后，在新 B03 目录记录范围、接口与验收矩阵，再实施和验证项目管理。
- [ ] Phase D: Keep the audit trail
- [ ] Phase E: Verify and hand back

完成判据为 B02.6 必需 LIVE-01 至 LIVE-10 真实证据完整且经独立复核，以及 B03 授权范围实现并通过用户列明的 PostgreSQL、HTTP、浏览器和工程验收。准备、实现、本地验证、真实外部验收与整批 VERIFIED 分别记录。

## 吞吐检查点

- Blocking first steps. 接手时间仍早于 2026-09-13T00:58:45.991534Z 自然刷新窗口约七小时。先核对运行证据，不重新登录、重连或修改期限。第二账号外部变更与 B03 产品实现均依赖该门槛。
- Independent workstreams. 主代理完整阅读指定文档并检查现场。独立模型只读复核已生成证据和本轮记录，不操作浏览器或数据库。
- Shared mutable state. 主代理只写本轮新目录及必要的当前交接。原 observer 独占 refresh-02 的运行输出，原 keeper 独占其日志。禁止重复启动、覆盖历史证据、改写配置或发布包。
- Smallest safe decomposition. 先完成接手核对，再等待真实刷新及 LIVE-10，之后执行第二账号场景和强制恢复。每一门槛实际通过后才进入下一步。

主代理承担规划与编排。B03 架构和接口由 GPT-6 Astra 负责，实现由 gpt-5.6-sol 负责。未承担主要实现或真实操作的其他模型负责独立复核。禁止暂存、提交和推送。

用户随后授权提前独立开发 B03。原计划等待 B02 后实施 B03 的条目改为等待 B02 后集成验证，独立开发已在 `/home/xubohan/projects/Repomesh_B03` 开始。
