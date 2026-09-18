---
status: research
date: 2026-09-10
source_commit: 9d546e331dc9ca018dac3332b1f2ac1981bc009f
---

# 旧项目分析能力的插件化可行性调查

**调查后的采用范围：** 用户随后选择先在创建 Issue 时提供“仓库分析”按钮，以 Python 插件实现，已记录到[现行专题](../current/issue-creation-repository-analysis.md)与 [ADR-0020](../adr/0020-python-repository-analysis-plugin.md)。以下保留调查时的建议语境；“两个逻辑插件”没有被整体采纳，当前只确认仓库分析，历史决策分析与向量底座仍属后续。下文“尚未采纳”的进程扩展，现已由 ADR-0020 在上述限定范围内采用。

本文回答：旧项目 `D:/Project4work/GOAI-infra-repomesh` 的“多仓库相关分析”和“历史决策分析”，能否以插件形式加入当前 Go 版 RepoMesh。结论属于源码调查与设计建议，不是已采纳 ADR，不表示完成迁移或运行验收。

**可以插件化。旧项目已经提供了适合拆分的模块和 Port 边界，但没有提供可直接搬来的分析插件安装平台。** 结合保留旧项目能力的目标，建议采用一个受控的 Python 分析运行包，提供两个逻辑插件，共用检索基础；正式身份、业务记录、权限、作业及结果采纳由 Go 核心管理。这个额外的分析宿主属于新增进程形态的设计扩展，尚未采纳，不能当成 ADR-0013 已确认的安排。若坚持现有三类 Go 进程，则改为 Go 静态注册的内置插件，并移植旧算法与契约测试。

## 调查范围和证据强度

- 旧库 HEAD 为 `9d546e331dc9ca018dac3332b1f2ac1981bc009f`；本轮检查时 tracked 工作树干净。结论针对该工作树。
- 读取根 `AGENTS.md`、相关源码、模块元数据、装配入口、数据库模型与迁移、部署文件、测试源码，以及新库 ADR-0010／0013／0014。
- 没有启动旧库服务、调用模型、读取 `.env`／`.secrets`／`admininfo.txt`，没有修改旧库。测试文件仅作为预期契约的辅助证据，本轮没有运行测试。
- 本文的“未发现”限定于所检查的旧库自有 `src`、`pyproject.toml`、`capabilities`、装配入口和相关测试；不据此断言任何上游组件也不存在扩展机制。

## 旧项目实际有什么

### 模块边界存在，动态插件生命周期尚不存在

`repository_intelligence` 声明自己拥有仓库注册、版本化画像、依赖图和仓库发现；`decision_chain` 声明自己拥有决策链投影、追踪和结构相似性搜索。两个模块均为 active，并各自声明 PostgreSQL schema。[仓库模块元数据](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/repository_intelligence/module.toml:1)、[决策模块元数据](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/decision_chain/module.toml:1)

`module.toml` 目前是归属和职责元数据，不是可执行插件清单。架构测试检查文件存在，并约束跨模块 import 只能指向公开 contracts；实际服务仍由 Python composition root 手工导入和构造，HTTP 路由也逐项注册。本轮未找到按 `module.toml` 动态加载分析能力、兼容性协商、安装／禁用／卸载分析包的实现。[边界测试](D:/Project4work/GOAI-infra-repomesh/tests/architecture/test_module_boundaries.py:66)、[分析服务装配](D:/Project4work/GOAI-infra-repomesh/src/repomesh/bootstrap/container.py:939)、[静态路由注册](D:/Project4work/GOAI-infra-repomesh/src/repomesh/api/router.py:44)

项目使用 Python 3.12+，依赖 FastAPI、SQLAlchemy、asyncpg、httpx 等；`project.scripts` 声明几个应用命令，没有分析插件 entry point 注册组。当前分析模块也未独立打成插件发行包。[Python 包定义](D:/Project4work/GOAI-infra-repomesh/pyproject.toml:6)

因此，迁移可以复用内部接口、算法与契约测试，但不能描述成“把两个现成插件复制到 Go 项目就能启用”。

### 历史分析和仓库分析已有明确连接点

仓库发现依赖 `DecisionHistoryPort`，输入组织、当前项目、候选仓库与查询文本，输出带决策身份、项目身份和业务时间的历史摘要。Port 明确历史只读、可降级；同模块没有直接导入另一模块的表实现。[历史查询 Port](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/repository_intelligence/ports/decision_history.py:19)

composition root 将决策链的结构检索或语义检索适配为该 Port，再注入发现服务。这是迁移时最有价值的拆分点：仓库分析消费历史依据，两个能力仍各自负责自己的规则。[历史适配装配](D:/Project4work/GOAI-infra-repomesh/src/repomesh/bootstrap/container.py:939)、[发现服务注入](D:/Project4work/GOAI-infra-repomesh/src/repomesh/bootstrap/container.py:1708)

旧项目“历史决策”首先是业务事件的读侧投影：classification → confirmation → integration → task → pr。决策生产者仍拥有正式事实，投影保存摘要与证据引用；它不是扫描 ADR 文档并自动推断生效／失效关系的完整系统。[Decision Chain 范围](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/decision_chain/README.md:3)、[事件与需求读取 Port](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/decision_chain/ports.py:71)

### 旧库有向量检索逻辑，但没有已部署的 pgvector 底座

多仓候选发现的主路径本身并不依赖向量：它读取仓库画像，优先用LLM评分，失败后用IDF加权关键词覆盖率，再结合结构依赖图补充候选。向量主要用于为分类查找相似历史决策。因此两项能力可以共用检索，但不应把“部署向量库”作为多仓分析能够工作的前置条件。[候选发现](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/repository_intelligence/application/discovery.py:110)、[关键词回退](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/repository_intelligence/application/discovery.py:297)、[历史参考调用](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/repository_intelligence/application/discovery_chain.py:761)

`decision_embeddings` 将 `embedding` 保存为 JSONB；每次读取组织范围内已向量化的决策，随后在 Python 中按仓库范围过滤、计算余弦相似度，并按历史项目折叠结果。不是 pgvector 列、SQL 向量索引或独立向量数据库。[实际存储类型](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/decision_chain/infrastructure/models.py:69)、[数据读取](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/decision_chain/infrastructure/embedding_store.py:70)、[Python 排序](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/decision_chain/application.py:364)

Embedding 来自配置的 HTTP `/embeddings` 服务；未配置时工厂返回 `None`，发现链路退回结构检索。旧包没有要求把一个本地向量模型装进业务进程。因而“需要向量”不能单独作为必须保留 Python 运行时的理由。[Embedding 适配器](D:/Project4work/GOAI-infra-repomesh/src/repomesh/integrations/llm/embeddings.py:26)、[可选装配](D:/Project4work/GOAI-infra-repomesh/src/repomesh/bootstrap/container.py:962)

这里需要校正前一轮建议的语境：**PostgreSQL＋pgvector 是新架构可选的存储升级建议，不是旧项目已具备并可直接迁移的实现。** 索引存储应该在自己的 Port 后面，分析能力无需因替换 JSONB、pgvector 或其他实现而改变业务契约。

### 刷新和卸载不能照旧实现直接推定

向量批量刷新服务存在，显式管理接口可调用它；`DecisionEmbeddingRefresher` 也定义了定时循环。但本轮源码搜索未找到该循环在默认 bootstrap 被实例化并启动的接线。实际默认装配的是决策事件投影器，不能据类名推定向量已经自动保持最新。[刷新服务](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/decision_chain/application.py:328)、[定时循环类](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/decision_chain/application.py:443)、[显式刷新接口](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/decision_chain/api/router.py:282)、[默认事件投影器装配](D:/Project4work/GOAI-infra-repomesh/src/repomesh/bootstrap/app.py:778)

已有向量行以 `decision_id` 为键，模型中没有独立的 embedding 模型／维数／索引代次字段。当前 pending 查询面向尚未保存向量的决策，不能视为已经实现模型切换后的全量重建与双版本发布。[向量模型](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/decision_chain/infrastructure/models.py:80)、[刷新规则](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/decision_chain/application.py:348)

数据库迁移的 downgrade 会删向量表，它是部署迁移回滚，不是业务插件的安全卸载协议。没有发现“停新作业→等待／停止在途作业→撤销调用权限→保留历史报告→按指定代次回收索引”的完整生命周期。[向量迁移](D:/Project4work/GOAI-infra-repomesh/migrations/versions/20260901_0053_decision_embeddings.py:45)

## 四种“插件”需要分开理解

| 名称 | 实际职责 | 本次两个能力适合放在哪里 |
| --- | --- | --- |
| RepoMesh 分析插件 | 接收范围明确的材料，生成有证据的分析结果 | 建议的主归属；仓库分析与历史分析分别提供能力。 |
| Graph／Loop 插件 | 使用有效计划和观察计算跨仓放行、结果适用性与循环停止条件 | 消费已经采纳的分析依据；不应承载语义索引、历史知识库或替代正式执行校验。 |
| AgentTeams 的工具／MCP 扩展 | 让 Manager、Leader 或 Worker 调用外部能力 | 可以提供受控分析查询入口；不是业务事实、索引归属和插件安装状态的所有者。 |
| Skill／能力预设 | 为角色分配技能说明与可调用工具 | 可以说明何时调用分析，但不能单靠一份 Skill 完成持久索引、数据隔离和历史记录。 |

旧 `capability_management` 明确自己是 Skill／MCP 能力控制面，不是 MCP 客户端；其 bundle 是按角色装配的 Skill 与 MCP 定义。`McpCallGuard` 管调用超时、重试和审计，Worker MCP 路由只导出受控任务启动。这些是可参考的入口治理机制，不能当成业务插件宿主。[能力模块说明](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/capability_management/README.md:3)、[能力契约](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/capability_management/contracts.py:47)、[调用保护](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/capability_management/mcp_guard.py:1)、[现有 MCP 入口](D:/Project4work/GOAI-infra-repomesh/src/repomesh/api/worker_mcp.py:13)

## 推荐的加入形式

以下是建议的新契约边界，不是旧库已经具备的接口。

```text
页面 / Agent 的受控工具调用
              |
              v
Go 核心：核验范围、固定输入版本、登记作业与预算
              |
      版本化分析契约 / 受控 Python 宿主
       /                      \
仓库相关分析                历史决策分析
       \                      /
        共用检索与证据访问 Port
              |
Go 核心：校验结果来源，保存分析报告，决定是否采纳
              |
有效计划 / Graph 计算 / 后续业务操作
```

建议对外提供两个能力，例如 `repository-analysis` 和 `decision-history`；内部共用 embedding 与检索适配。一个发行包可以包含两者，用户可以分别启用，物理上不必因此部署两套向量数据库或两个常驻服务。

核心保留组织／项目／Issue／仓库身份、正式事件和决定、权限、作业、预算与结果采纳。插件拥有可重建的分析投影、缓存、索引代次和算法实现；插件输出只成为带来源的建议或证据，不自行确定正式仓库范围、应用计划或派 Worker。复用旧链路时，尤其应切开发现分析与后续 materialization，不能把业务执行也随分析能力打包迁入。[旧分析与物化装配分界](D:/Project4work/GOAI-infra-repomesh/src/repomesh/bootstrap/container.py:1694)

### 运行方式选择

| 方案 | 优点 | 需要付出的成本 | 与当前决定的关系 |
| --- | --- | --- | --- |
| Go 内置插件：接口＋静态注册＋配置启用 | 与后端一起发布；无需新增语言宿主；核心可以直接管理作业和凭据 | 旧 Python 算法和测试需要迁移；不能独立升级；没有进程故障隔离 | 最接近现有 Go 模块化单体方向；两个新模块的具体职责和接口仍需设计。 |
| 一个受控 Python 分析宿主，提供两个逻辑插件 | 可逐步提取旧 Python 分析实现；独立依赖与资源限制；未来可以独立替换 | 必须抽离旧数据库／事件／Web 装配；新增进程协议、兼容性、运行监控和超时恢复 | 可行，但属于 ADR-0013 之外的新进程形态，需要另行记录设计扩展。 |
| 全部放进 AgentTeams Worker 内 | Agent 可以直接调用现有工具 | 分析能力与 Worker 生命周期、权限、模型会话和实例存储耦合；正式分析记录难以统一 | 不建议作为持久分析服务的主归属；仅保留调用入口。 |

**针对这次复用旧项目能力的目标，建议先定义稳定契约，再提取为一个受控 Python 分析运行包。** 一个宿主提供两个逻辑插件，避免先重写全部分析实现，也不为两个能力各建一套常驻服务。如果优先保持原三类 Go 进程，则选择 Go 内置模块并移植算法。Python 的理由是保留旧实现，不是向量检索技术本身要求 Python；当前向量客户端和余弦检索并无必须留在 Python 的重型本地模型依赖。

如果选 Python 宿主，应包装所需的纯分析服务及专用适配器，而非启动整个旧 RepoMesh API。Go 通过有限的版本化请求提供快照和范围；分析宿主通过专用检索权限访问自己拥有的索引，不能直接修改 Go 核心业务表。同步查询和异步作业都沿同一能力契约返回结构化结果；MCP 可以作为 Agent 入口，首期无需同时新增一套 MCP 和一套后台专用协议。

### 第一版契约必须回答的事项

| 契约 | 至少需要包含的内容 |
| --- | --- |
| 能力身份与兼容性 | 插件 ID、实现版本、契约版本、提供的操作、需要的来源与索引格式。 |
| 输入范围 | 经核心核验的项目／Issue／仓库范围，固定 commit／文档修订，查询文本和目标；调用参数本身不授予访问权。 |
| 索引身份 | 来源内容哈希、解析器版本、embedding 模型与维数、索引代次，当前已覆盖到的版本。 |
| 输出与降级 | 相关对象、证据引用、推断理由、覆盖范围；`complete`／`partial`／`unavailable` 分开，不能把未索引误写成没有关系。 |
| 长任务与恢复 | 稳定作业 ID、幂等键、状态查询、取消与实际停止确认、结果发布条件；索引失败不得破坏当前可用代次。 |
| 启停与删除 | 禁用时停止新调用与索引作业；卸载程序与删除分析数据分开；历史报告和正式决策留在核心，索引回收按插件／项目／代次明确执行。 |

这些不是要求先开发通用插件市场。两项第一方插件就可以验证上述契约；来源签名、任意第三方动态装载、热更新和市场安装不在本次迁移的必要范围内。

## 与已采纳 ADR 的关系

ADR-0010 已选择 Go 自有后端、PostgreSQL、持久后台队列和模块化单体，但没有选定向量数据库或动态插件框架。把分析能力组织为 Go 内部模块，符合这一组织方向；不能据此宣称 pgvector 已经正式选定。[ADR-0010](D:/Project4work/Repomesh_Go_ver/docs/adr/0010-technology-stack-and-modular-monolith.md:15)

ADR-0013 已采用同一 Go 工程配套发布的 Web、后台协调、受限主机执行三类进程，并说明不为每个领域模块建立独立网络服务。因此若选择额外的 Python 分析宿主，需要明确其例外范围、生命周期管理者、凭据和资源限制，再记录新的决定；本文不改写既有 accepted ADR。[ADR-0013](D:/Project4work/Repomesh_Go_ver/docs/adr/0013-web-coordinator-host-executor-processes.md:9)

ADR-0014 的进程内要求针对 Graph／Loop 插件，职责是跨仓协调和循环策略，不是所有未来分析插件的通用运行政策。该 ADR 也明确没有选定 Go 动态插件、第三方插件框架或任意热替换。不能因为“分析也画图”就将向量检索塞入 Graph 模块。[ADR-0014](D:/Project4work/Repomesh_Go_ver/docs/adr/0014-in-process-graph-plugin.md:15)

## 迁移时优先保留与补齐

### 可提取的多仓分析资产

旧扫描已实现构建依赖、运行调用声明、共享资源、部署配置、跨仓源码引用五类证据；运行观测类仍是保留位置。仓库身份／别名解析和确定性依赖图区分confirmed与declared，只有confirmed参与拓扑约束，共享数据库不自动推导调用顺序。这些解析器、证据分类与图规则是值得提取的资产，不能只把旧提示词当作可复用内容。[证据模型](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/repository_intelligence/domain/models.py:14)、[扫描调用](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/repository_intelligence/application/scan_remote.py:278)、[依赖图](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/repository_intelligence/application/dependency_graph.py:34)

目前DepEvidence只有name/mechanism/confidence，没有commit、文件位置和原文哈希；Fetcher读取接口未显式固定ref，路由扫描也有最多30个源文件的边界。新插件应接收固定commit的材料，并逐项输出来源定位和未扫描范围。[证据字段](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/repository_intelligence/domain/models.py:27)、[Fetcher接口](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/repository_intelligence/infrastructure/platform/fetcher.py:96)、[有界扫描](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/repository_intelligence/application/scan_remote.py:239)

旧分析任务的单飞和运行记录使用进程内字典及asyncio.create_task，不提供新架构所需的持久任务恢复；分类JSON解析失败又可能返回REQUIRED及0.5置信度。迁移时分别用核心持久作业和显式parse_error／degraded结果承接，不能原样当成可靠插件调度与正常分析结论。[进程内任务](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/repository_intelligence/api/discovery_chain.py:105)、[解析失败路径](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/repository_intelligence/application/confirmation.py:312)

### 历史语义与权限需要重新核对

旧链的upstream_ref链接前一个业务步骤，不是决策替代关系；枚举含SUPERSEDED也不代表已实现生效／失效时间推理。当前embedding文本主要是步骤、状态、仓库、任务或PR等决策简表，而不是ADR、讨论或Git历史全文。[链链接](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/decision_chain/infrastructure/_links.py:67)、[文本构造](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/decision_chain/application.py:294)、[状态投影](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/decision_chain/application.py:501)

旧全局审计接口以共享agent_action_token保护，允许部分查询省略组织范围；这不能直接变成新产品面向各用户／Agent的细粒度查询权限。另有similar的semantic API未传same_repository_ids，而分类pipeline的向量适配器明确传入；不能把后者的同仓限制推广到所有调用路径。[审计入口](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/decision_chain/api/router.py:36)、[API调用](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/decision_chain/api/router.py:194)、[pipeline过滤](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/repository_intelligence/infrastructure/decision_history_vector.py:108)

### 迁移验证基线

已有行为测试覆盖依赖证据、身份冲突、图约束、事件幂等与版本、向量排序和历史提示注入，适合作为迁移样例。它们使用ScriptedLLM、固定向量、fake HTTP等；名为Postgres语义对照的embedding测试实际经application_container使用SQLite，不能据名称宣称真实PostgreSQL或模型质量已验证。本次未执行测试。[图测试](D:/Project4work/GOAI-infra-repomesh/tests/test_dependency_graph.py:108)、[向量存储测试](D:/Project4work/GOAI-infra-repomesh/tests/test_decision_chain_embeddings.py:659)、[数据库fixture](D:/Project4work/GOAI-infra-repomesh/tests/conftest.py:21)

优先保留旧库已写清楚的边界：分析提出范围、核心采纳范围；正式决定由业务事件生产者拥有；历史链只保存投影与来源；仓库分析通过 Port 使用历史依据；embedding 不阻塞正式事实写入。

需要补齐的内容包括：旧 `project_id` 代表一条需求决策链，应映射为新 Issue 及其分析轮次，不能直接当成新版长期 Project；还需明确当前权限约束、固定仓库版本输入、索引代次和模型切换、来源失效与删除传播、降级状态，以及插件禁用和在途作业处理。旧接口中 `organization_id=None` 表示跨组织查询、仓库关系使用 slug 等约定，不应未经重新设计就成为新插件的外部授权协议。[旧需求链身份](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/decision_chain/README.md:13)、[旧查询范围语义](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/decision_chain/ports.py:46)、[旧仓库标识语义](D:/Project4work/GOAI-infra-repomesh/src/repomesh/modules/repository_intelligence/ports/decision_history.py:60)

旧发现服务的人工审批、旧全局凭据和后续任务物化不随分析代码迁移；结果有效性仍按当前 Go 版的计划、权限和输入版本规则核验，不能因复用旧分析链恢复已经暂缓的审批模块。

若目标还包括“历史 ADR 是否被替代、当时为什么决定、现在仍适用什么”，应另加文档决策的来源适配、适用范围与生效／替代关系。旧五阶段业务链可以提供追溯基础，但不能直接等同于完整的时间有效性推理能力。
