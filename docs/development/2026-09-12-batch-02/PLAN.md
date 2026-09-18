# B02 实施计划

- [x] Read the Principles section of the poteto-mode skill.
- [x] Phase A: Frame
- [x] Phase B: Design the workflow
- [x] Phase C: Run the loop
- [x] Phase D: Keep the audit trail
- [x] Phase E: Verify and hand back

## Architect

1. Ground. 复用前置核对的 Web、数据库和候选依赖结论，核对现有源码。
2. Sketch. 两个独立候选分别比较状态和事务归属。
3. Agree. 用户已采用产品范围，实现形状由比较后确定，无需再次确认。
4. Implement. 按下列单元实现，每单元验证。
5. Scrap. 若事务和代次模型不能覆盖竞态，重画相关部分。

## Arena

1. Frame. 比较契约完整性、并发与未知恢复、模块边界、可运行验证和实现成本。
2. Fan out. 候选写各自临时目录。
3. Cross-judge. 独立模型只读比较。
4. Pick. 选定基础形状。
5. Graft. 合入另一候选的必要约束。
6. Verify. 用真实 PostgreSQL 和 HTTP 故障用例验证。

## 实施单元

| 单元 | 交付 | 状态 |
| --- | --- | --- |
| B02.1 | 认证秘密基础、迁移、根自检与轮换 | LOCAL_VERIFIED |
| B02.2 | 稳定账号、绑定、会话、尝试、登录重连及刷新 | LOCAL_VERIFIED |
| B02.3 | 仓库发现批次、游标、coordinator 持久续扫 | LOCAL_VERIFIED |
| B02.4 | HTTP 组装、登录结果与恢复页面、配置说明 | LOCAL_VERIFIED |
| B02.5 | 本地完整验证与独立复核 | LOCAL_VERIFIED |
| B02.6 | 真实 GitHub OAuth 及浏览器验收 | BLOCKED，GitHub App 尚未配置 |

## 吞吐检查

- Blocking first steps. 固定存储、状态和依赖接口后再并行实现。使用本批专属 PostgreSQL，保留 B01 的验证语义。
- Independent workstreams. 秘密、GitHub 适配、前端可在接口固定后独立开发。主代理负责集成和认证状态，验证代理只读复核。
- Shared mutable state. 各写入代理使用独立临时副本，只交回指定文件。迁移编号和共享入口由主代理分配，单一集成者写主目录。
- Smallest safe decomposition. 先验证秘密基础，再验证持久认证与发现，最后组装页面。整个 B02 未取得真实外部证据前保持 IN_PROGRESS。

Phase E 验证并交回的是本地实施包。B02.6 仍受真实 App 配置阻塞，未将整批标为 VERIFIED。证据见 [README](README.md)。
