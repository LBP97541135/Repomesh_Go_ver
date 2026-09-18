# 方案比较与推荐

状态：DESIGN_RECOMMENDATION_NOT_ADOPTED。Astra 负责综合。既有采用规则直接沿用；本表 D01—D08 是需本轮交回用户取舍的新推荐，不能据本文件开始实现。

| 决定 | 方案 A | 方案 B | 推荐、适配成本和替代关系 |
| --- | --- | --- | --- |
| D01 目录归属 | 将 B03 配置解析迁至新 configurations 包，models/sources/projects/issues 全部依赖它 | 保留 projects 对目录和项目固定配置的所有权，新增有限的事务内目录写入／配置读写接口；models/sources 不复制解析器 | 推荐 B。现有表、私有 fixedConfiguration 和 B03 调用集中在 projects；B 可少搬迁现有稳定代码。跨包入口必须完成一个目录登记或项目配置变更，不能暴露一串 SQL setter。A 更适合将来确有独立配置消费者后评估，本轮不预铺通用配置服务。 |
| D02 秘密比较 | 候选 A 的随机 MAC key＋InputVault，命中 MAC 后精确比较 | 一个独立目的的加密完整规范化输入 vault；解密后精确比较，keep 保存非秘密规范化输入 | 推荐 B。两者都能精确比较且不绕过业务 Key 吊销；B 少一个秘密版本及一次包装，无裸 Key 哈希。每次 replace 成功有业务密文＋vault 两份，清理分别归属。替代旧内部稿 §3.1 MAC 方案及旧 A，收敛中断 final 的方向。 |
| D03 秘密事务 | 继续 Seal 独立提交，业务失败后清理孤儿 | Prepare 在无业务锁时预扣包装并加密，InsertPrepared 加入用例事务 | 推荐 B，沿旧 A/final。A 会留下可见的半套秘密及恢复责任，不能满足保存原子性。B 复用现有 envelope/AAD/计数，保留现有认证 Seal API；后续仅其内部复用新方法，不重写认证协议。 |
| D04 来源和默认 | 原 S03 全量多 owner 清单同时启用 App/预算/出站/容量 | B04 schema 1 每项单一 owner 的私有执行元数据（清单可有多个 owner，排序锁定）；B05 schema 2 增加必要不可变时限／次数政策和出站登记 | 推荐 B。不引入共享 Provider 和配置后台。B04 允许材料不完整的固定版本，B05 补全必须新 execution version。默认绑定显式 pinned_version；旧 null 默认保留 B03 历史语义，新导入不允许 null。替代 S03/S04 对 B04 的全量采用推断，不撤销 B02 App 配置渠道。 |
| D05 测试预算和外发 | 金额预算＋报价／用量核算；进程恢复自动尝试完成一次请求 | 有限 request 次数，单 testId 仅一张发送许可；可能发送后只核查 | 推荐 B，沿 S05/M04—06 候选。金额仍未知，不能承诺最大货币支出。5分钟预览、≤16输出 token、20次/UTC日、最多1笔未核责任及30秒请求期限均需明确采用；配置中必须写值，不自动填示例默认。关闭未知仅在旧发送者能力已撤销有证据时，由受控维护用例审计完成。 |
| D06 应用与摘要 | 复用完整 PATCH；另查策略服务拼摘要；原模型用 null 占位 | 专用应用复制 execution 原绑定和参数；项目 GET 内返回 C05 固定摘要；C06 可读/未配置/受限分型 | 推荐 B。满足已采用的 F04 目标，避免 execution 默认漂移及跨请求混版本。新增 HTTP 只读字段仍待采用，唯一字段分别归首批契约和模型契约。 |
| D07 P9 绑定 | Issue 自带完整配置副本，或按创建时间推定项目版本 | Issue 非空复合外键引用既有 ProjectConfigRevision，操作内部结果同版本 | 推荐 B，沿 P9。副本会制造两处权威配置和秘密保留责任；按时间推断没有事务证明。数据库约束验证同项目、一致结果、不可改写；不向既有创建 HTTP 增加字段。 |
| D08 Issue 关系与事务 | 多模块各自保存会话、Issue、CS，再补偿 | issues 共享本地用例持有一个短事务；在该模块内保存必要业务关系和待办、事件 | 推荐 B，具体物理约束为本轮新推荐；原子业务范围本身已采用。首批不建通用会话/消息/工作流框架；将来真实消息实现再按稳定关系分包，不允许当前独立事务。 |

## 直接沿用的决定

B02 信封加密与包装根分离；B03 owner-only、完整范围保留、显式双配置重解析、不可变修订和原回执、当前核权；F04 只换模型并保留 execution；Issue P1 原子范围、创建 201/重放200、唯一原键、清理占位、完整内容范围核权；ADR-0013/0016/0019 的进程与对象边界。模型 M/S 候选的具体协议、P9 与 C05/C06 仍未采用。

## 关键冲突的定点处理

- 旧 A 在 owner 后锁 import 单例，中断 final 改为单例先行。采用推荐：所有导入先取单例，查原键后才按 owner ID 排序锁账户；交互事务永不取该单例，导入持有它时也不取 binding/session/project，消除倒序。
- 原模型内部稿用业务 SecretVersion 比较成功 replace，业务 Key 停用后会影响原操作比较。D02 vault 有独立 owner/purpose，任何调用方都不能将 vault 当模型凭据。
- 原 final 用不存在的 web.Server 接收者声明 handler。当前源码用 registerProjects + ServeMux；本轮声明沿当前注册方式。
- 原来源默认包含 version，但 B03 defaults 只存 ID，resolver 读取 head。本轮新增 pinned_version，保留旧 null 的历史读取，显式新导入固定版本。
- B04 v1 执行材料无预算/时限，不能通过 B06。B05 v2 增加新不可变执行版本与政策来源，禁止 UPDATE 旧 profile_versions 或 fixed JSON 补值。
- B03 inspectProfile 固定 INTEGRATION_NOT_AVAILABLE 是已实现阶段事实。B05/B06 增加管理条件投影，只有完整材料与当前观察通过才允许新建；运行接入状态不混入配置材料完整性。

## 本次取舍的停止点

推荐已足以审阅和拆分实现任务；仍需用户明确采用 D01—D08 中的新产品选择，尤其 D04、D05、D06、D07。当前不执行实现。若拒绝次数预算，B05 测试额度及 C05 预算分支、B06 预算门槛一起退回设计；不得只改页面单位。

补充 S06 推荐：B06 新建所选仓库最低 Metadata read、Contents write、Pull requests write；用户与App观察均在提交前60秒内。它是待采用门槛，原结果查询只需要当前完整内容读权。若选择不同能力/时限，access观察、options和创建最终检查须一起调整。

D05的处理器可用性补充亦待采用：有限coordinator handler登记/60秒租约/20秒续期，只用于新测试资格；不将租约到期作为旧发送者能力撤销证明。首批apiFormat仅openai_chat_completions，扩展协议另审。
