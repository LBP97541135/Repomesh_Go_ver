# 独立复核意见的裁定

复核员：grok 4.6 子代理，只读，依据 HTML 方案文本、CONTEXT.md、ADR 与三份已采用契约。意见按 A（方案误读）、B（内部矛盾）、C（ADR 误用）、D（状态机）、E（文风）编号，共 59 条：A 18、B 13、C 10、D 8、E 10。根代理逐条裁定后合并成 30 条修订交写作子代理落实（提交 `e1c1ef6`），驳回 4 条，其余 25 条并入某条修订或与被驳回条目同因。

## 修订与意见的对应

| 修订 | 覆盖的意见 | 内容 |
| --- | --- | --- |
| 1 | B1 | `天然` 定义补 PATCH |
| 2 | B2、B3、B13 | 2.1 父资源、PATCH 改无迁移表的状态列、探针除外 |
| 3 | B8 | `公开` 含探针 |
| 4 | B4、B5 | PUT 首次 201、软删 DELETE 返回 200 |
| 5 | B12 | 唯一列列表补全 |
| 6 | B6 | 允许资源小节声明排序列 |
| 7 | A5、B7、A1、A4 | SSE 去掉 `projectId`；新增发现第 23 条（`events` 无 `project_id`，`tasks`、`messages` 无时间列） |
| 8 | A17、B10 | 2.8 第 1 类补三列，附录 D 同步 |
| 9 | A2 | `change_sets.task_id` 可空 |
| 10 | A3、A8 | 两个可空外键的 POST 标可省略 |
| 11 | A7 | `agent_teams.team_name` 进 PATCH |
| 12 | A9、A11、A14、A15 | 必填列的创建初值 |
| 13 | A6 | `events.correlation_id` 必填与后台取值 |
| 14 | A10、A12、A13 | 分组计数与唯一性标为提案 |
| 15 | A16 | 回避判断按解析后的身份比较 |
| 16 | A18 | `decision_embeddings` 逻辑关联与物理外键分开 |
| 17 | C5 | 会话响应形状不再沿用首批契约 §2 |
| 18 | C6 | ADR-0005 §1 引用缩小 |
| 19 | C7 | `plan_steps.depends_on` 只创建时写 |
| 20 | C8 | YOLO 自动许可写入 `review_requests` |
| 21 | C4 | 附录 B ADR-0019 行改为未落实 |
| 22 | C2、C3 | 第 8 节状态说明、H1/H11/H10 差异 |
| 23 | D1、D2、D3 | `tasks` 迁移表补洞 |
| 24 | D4 | `skill_versions` 的 `draft` 可废弃、驳回置 `deprecated` |
| 25 | D5、D8 | `change_sets` 再交付与 `abandoned` |
| 26 | D7 | `recovery_operations` 失败与超时的 `result` |
| 27 | E3 | 3.2 权限格 |
| 28 | E4 | 6.1 用途格 |
| 29 | E8 | 2.7 用语 |
| 30 | E9 | 4.4 start 与 stop 拆句 |

根代理另改一处：`change_sets` 从 `delivering` 失败退回时按 `deliveryStatus` 决定回到 `open` 还是 `delivered`（写作子代理在落实修订 25 时指出）。

## 驳回

| 意见 | 理由 |
| --- | --- |
| C1（第 8 节违反 ADR-0009，应删除可实施端点） | 用户已把含 7 张 Skill 表的方案定为正式方案并要求据此设计 API。第 8 节是随方案给出的设计，已明写 ADR-0009 更新前不得实施；删除会让 44 张表的映射不完整。 |
| C10（`semantic-search` 读路径计算查询向量违反 ADR-0021） | ADR-0021 决策 5 限制的是写路径不调用 LLM；读路径为查询文本计算向量是现有 `GET /api/decision-chains/semantic-search` 的既有行为。 |
| D6（`alert_events.handling` 对浏览器只进不出） | `handling` 由后台自动处置写入，`resolve` 允许从 `handling` 到 `closed`，状态可达也可退出。 |
| B9、E5（`项目管理员` 与 `组织管理员` 两个名字） | 两者是两个事物：ADR 要求的设计角色与方案缺表时的暂代执行角色。2.2 已分别定义。 |

## 未单独处理

C9（ADR-0021 未入索引、与 ADR-0020 的关系）已是附录 E 第 16 条，属用户裁定。E1、E2、E6、E7、E10 是可改善的文风意见，本轮未改：E6 涉及角色称呼，等附录 E 第 1 条裁定后统一；其余在下次修订时处理。
