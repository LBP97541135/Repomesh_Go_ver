# 全本地观测、评分与样本复核工作台

本轮沿用 [ADR 0024](../adr/0024-local-observation-workbench.md)，并按用户最新要求把观测、调度、数据集和人工标注全部放在本机。Jev API 专责 rubric／数值评分；DeepSeek API 用于主题聚类、证据归因和 AI 初标。这里是 RepoMesh 的本地实现，不依赖 AgentLoop 云空间、云控制台或云评估任务，也不表示模型权重本地推理。

新增功能依据见[增量 Spec](observation-platform-expansion-spec.md)，实施和真实模型验收见[本轮记录](../development/2026-09-20-local-observation-jev/README.md)。原生 DSH／真实跨仓实验仍独立验收。

## 启动

```bash
# 标准管理脚本；从当前源码构建独立工作台
bash scripts/observe-workbench.sh install
bash scripts/observe-workbench.sh start
bash scripts/observe-workbench.sh status

# 或使用自己的私有目录及未占用端口
# 目录权限必须为 0700；只监听回环
go run ./cmd/repomesh-observe serve \
  --archive /absolute/private/observation/archive \
  --addr 127.0.0.1:19091
```

脚本默认使用 `$HOME/.local/state/repomesh-observe-local` 和 18090 端口；可通过 `REPOMESH_WORKBENCH_STATE`／`REPOMESH_WORKBENCH_PORT` 选择独立实例。原有服务不会因构建新 worktree 自动切换。本轮开发预览使用独立状态目录及 19091，具体位置见实施记录。

`--read-archive DIR` 可重复添加旧档案；读取不创建目录，Journal 层拒绝写入。新评分、样本和标注只写当前工作档案。

## 在主控制台维护模型

进入「设置 → 模型与 API」（`#/settings/models`），直接填写 Jev／DeepSeek 模型和 API Key，点击保存，再测试连接／读取可用模型。Key 留空保留当前值，首次配置必须填写；保存后输入框清空，刷新只读到模型名与配置状态。测试读取供应商模型列表，不执行付费推理，也不将列表检查写成评分成功。

Web 经同源认证接口调用本机工作台的已有保存端点，工作台的 `model.json`／`assistant.json` 仍是实际使用的唯一配置；不额外复制一套到浏览器或业务数据库。管理员权限、Origin 与 CSRF 逐请求校验。`REPOMESH_OBSERVE_WORKBENCH_URL` 在 Web 启动时读取，默认 `http://127.0.0.1:18090`，只接受回环 HTTP(S) origin；19091 预览需显式设为对应地址。工作台不可达会显示错误，不能显示假保存成功。 该目标位于 Web 所在主机；访问远程 Web 时，不会自动配置访问者电脑上的独立工作台。后者继续在其自身设置页维护，Key 不跨主机自动同步。

平台其余依赖未就绪时，这个设置入口仍可打开。独立工作台设置作为本机维护入口保留，并同样提供模型列表读取。仓库分析／项目执行供应商继续在同一分类下方管理，不与 Jev／DeepSeek 共用 Key。

## 页面与模型分工

| 页面 | 使用方式 |
| --- | --- |
| 任务地图（默认） | 先选择一次 Trial 或 OTLP Trace，再沿任务故事线、性能泳道和证据链理解结果；每一步可继续打开原始详情。 |
| 数据概览 | 查看本机记录、独立验收与明确的运行接入缺口。 |
| Trace 与事件 | 查看真实收到的父子／Links、时点事实和原始证据；有可信任务绑定的 Trace 可评分。缺父、未知身份不补造。 |
| 指标与计量 | 查询物理调用、TTFT、Token 与缺失原因；团队、Jev Judge、DeepSeek 分析分别统计。 |
| 评测与验收 | 运行固定折扣 HTTP 用例；结果为 pass／fail／unknown，不代表 Agent 能力。 |
| Rubric 评分 | 在验收／Trace 详情发起 Jev；查看维度、原始概率、置信度、未采纳原因和真实 usage。 |
| 本地聚类与归因 | 选 4—30 条冻结样本做 DeepSeek 主题归组；归因从具体样本发起，返回证据说明及 AI 初标。 |
| 样本与复核 | 保存选中的 Trace、失败、低分或抽检样本；查看初标并由人确认／纠正；导出自包含 JSON。 |
| 设置 | 分别保存 Jev／DeepSeek 模型和密钥；显式配置本地自动评分、每日付费调用上限和迟到窗口。 |

任务地图是现有数据的渐进式索引，不新增结论或改写档案。第一层显示一次执行的结果、耗时、模型评审和证据状态；第二层显示该执行的真实步骤、父子 Span 或验收阶段；第三层复用原详情窗，保留完整 Trace／Span 属性、事件、Links、逐项检查、实际 HTTP、Jev 概率与 usage、证据 JSON。九个完整数据页仍从左侧导航直接进入。Trial 没有共享运行时 Trace 时，性能页只列实际观测到的验收／Jev 耗时并明确缺口；Trace 性能泳道只使用上游提供的起止时间。

默认评分模型固定为 `jev-1.13.0`。Jev `/models` 只列别名，固定版本未列出不等于不可用；连接测试和实际评分分开显示。旧 DeepSeek `model.json` 可读取元信息及历史结果，但不能继续执行 rubric 评分；切换 Jev 必须提供对应的新密钥，不能复用 DeepSeek Key。

DeepSeek 默认模型为本轮账号实际支持的 `deepseek-flash`；可改为账号支持的其他 `deepseek-*`。DeepSeek 输出类别、说明与初标，不给 rubric 数值分。Jev 不生成解释文本；维度说明由代码与 typed answers 构成。

两个凭据分别保存在主档案中的 `model.json` 和 `assistant.json`，权限 0600。页面不回读明文密钥。原始证据在本机保留；发往模型的是选定材料的独立投影，屏蔽已配置密钥、常见凭据字段、嵌套 JSON 中的秘密及典型文本凭据／邮箱。脱敏策略与缺口进入固定输入记录；任意自然语言中的敏感信息无法仅凭模式匹配作绝对保证。

## Jev 评分与付费尝试

当前 rubric 为 `repomesh-evidence/3`，包含契约一致性、产物正确性辅助判断、证据可复核性、工具效率。每项使用独立 Score 和证据充分性 Choice。没有工具轨迹不编造效率；明确的固定 HTTP 产物不在 Agent 工具效率范围，标为 not_applicable，真实 Agent 缺轨迹则为 unknown。

原始 Score、legend、概率和 confidence 保留。归一化分数由代码计算；初始阈值只是辅助政策，尚需领域校准。Jev 判断不覆盖确定性验收，不触发人工批准或 Git 合并。

所有新 Jev／DeepSeek 模型操作在付费前保存不可变尝试，关联准确的输入、模型与结果身份。默认相同输入重复提交返回已有结果；发送后超时或结果落盘失败会保留未知尝试，不自动重复付费。界面的“Jev 重新评分”明确创建新尝试，旧评分继续保留。每日上限覆盖该实例的新付费尝试；它不是供应商账单的金额封顶承诺。

本地在线评分默认关闭。开启后每 5 秒检查一次，初始每日 10 次、终态后等待 5 秒；完整性要求是根节点完成、预期 Span 数匹配、父节点齐全且有任务／Attempt 绑定。夹具默认排除，Judge 永久排除。晚到材料产生新修订，已完成评分不改写。未知或中断的预约可以在本地状态里核对。

## 发现链模型计量接线

Web／coordinator 组合根支持下面两个环境变量；它们只影响下一次启动的新进程：

```bash
export REPOMESH_OBSERVE_ARCHIVE=/absolute/private/observation/archive
export REPOMESH_OBSERVE_SOURCE_ID=deployment-a
```

来源 ID 必须稳定且非秘密。启用后，发现链真实模型请求通过有界队列写本地 `model_calls`；网络重试、缺失 usage 和错误保持来源事实。来源键分别加 `/web` 和 `/coordinator`，不把不同进程实例混为相同部署。健康文件记录接收、去重、落盘、失败和丢弃数，并进入状态查询。

这条路径是非流式调用，因此 TTFT 为 `not_supported`。它精确到 Project／Issue，不能冒充 Task／Attempt 或 DSH turn 完整链；读面标为 `exact_issue` 并列出缺失字段。采集排队与落盘不在业务事务里等待云端。

## 本地 HTTP 契约

除标准 OTLP 接收外，管理写请求须带 `X-RepoMesh-Local: 1` 和 JSON；保留回环、Host／Origin 和同源检查。下表是独立管理工具 API，不提供云代理或任意路径读取。

| 路径 | 行为 |
| --- | --- |
| `GET /api/health`、`GET /api/catalog` | 原有健康与兼容档案视图。 |
| `GET /api/status` | 来源、采集健康、付费预约与结果、原生运行时缺口。 |
| `POST /v1/traces` | OTLP JSON／protobuf，16 MiB／10000 Span 上限，持久保存后确认。 |
| `GET /api/calls` | 分页调用明细；支持 q、project、issue、task、attempt、repository、role、model、purpose、kind、trace、subject_kind、binding，limit 1—500、offset。 |
| `GET /api/metrics` | 相同筛选的去重计量；已知 Token 小计、字段覆盖、未知整体覆盖率。 |
| `GET /api/trace?archive=…&id=…` | Trace 与准确材料修订，标记是否具备评分条件。 |
| `POST /api/bindings` | 本地管理者登记来源、Trace、Project／Issue／Task／Attempt 及派发证据；身份不可覆盖。 |
| `GET /api/rubric` | 当前冻结 rubric 和辅助阈值政策。 |
| `GET /api/settings`、`PUT /api/model`、`POST /api/model/test` | Jev 配置与非计费的模型列表检查；不回读 Key。 |
| `POST /api/judge` | archive 加 trial_id 或 trace_id，建议带 expected_revision；可带 attempt_key 实现明确重评及 HTTP 重放幂等。 |
| `GET/PUT /api/evaluation-policy` | 本地自动评分开关、每日调用上限、迟到窗口和明确的夹具许可。 |
| `GET/PUT /api/assistant` | DeepSeek 分析模型／私有 Key。 |
| `POST /api/assistant/test` | 使用已保存 Key 读取 DeepSeek 模型列表，不发起推理。 |
| `POST /api/analyses/attribution` | sample_id、expected_revision，可带 attempt_key；追加归因及 AI 初标。 |
| `POST /api/analyses/clustering` | 4—30 个 sample_ids，可带 attempt_key；校验分组成员完整性。 |
| `GET /api/analyses` | 本地分析记录、固定输入、结果和 usage。 |
| `POST /api/samples` | archive、trial_id 或 trace_id、expected_revision、reason，可带对应 judgment_id；同材料幂等，选样原因单独追加。 |
| `GET /api/samples`、`GET /api/samples/export?id=…` | 样本、选样原因、AI／人工标签、固定自包含材料。 |
| `POST /api/annotations` | sample_id、expected_revision、actor、verdict、category、reason、supersedes；只能确认确切样本修订，旧人工标签变化时返回冲突。 |
| `GET /api/evaluations` | 本地 Jev 及历史评分，只读查看。 |
| `POST /api/trials`、`GET /api/dataset.csv`、`GET /api/evidence` | 保留旧固定验收、CSV 和校验引用读面。 |

旧平台导出可以通过兼容的本地导入接口保留，但不存在云同步；云模式配置被拒绝。恢复时以档案里的原始身份和修订为准，不手写“成功”来消除未知预约。

## 验证与已知边界

相关自动检查包括只读档案、物理调用去重／冲突、指标缺失与单位、Jev 响应结构和 legend、付费重放与落盘失败、脱敏、样本修订、AI／人工角色、在线 Judge 排除及浏览器本地导航。任务地图的浏览器验证覆盖 Trial 与真实 OTLP 协议夹具两条路径、三级下钻、原页面保留、移动端和 CSP／外部请求边界；该协议夹具不代表真实 Agent 执行。具体结果见[任务地图实施记录](../development/2026-09-20-observation-task-map/README.md)及前序实施记录。

本地管理工具按私有档案工作，不是多租户服务。当前调用 API 提供分页，但档案扫描与兼容 catalog 仍以本机规模为目标。原生 DSH、真实流式生产者、跨仓候选生命周期到审核单的完整版本同步、真实任务效果与人工效率实验仍需后续验证。拥有 model_calls 或协议 Trace 不代表这些门槛已通过。
