# Skill 工程设计（待定占位）

更新：2026-09-08。**模块状态：待定／占位，暂不开发。** H1—H15 的设计方向已由用户同意并写入 [ADR-0009](../adr/0009-skill-engineering-deferred.md)。本专题保留组织说明、场景与上游静态证据；启动时间和实施范围后续另定，当前不开发、安装或部署 Skill 工程能力。

沿用 [ADR-0006](../adr/0006-manager-entry-modes-and-skill-driven-execution.md) 的 Skill、计划与实际执行分工，以及 [ADR-0003](../adr/0003-plan-change-authorization-and-activation.md) 的许可和生效规则。领域定义见 [CONTEXT.md](../../CONTEXT.md)。

## H1：独立 Skill、角色能力组合与事项工作说明

用户采用：**独立维护可复用 Skill，按角色组合能力，每条 Issue 引用确定版本并附本次工作说明。**

| 对象 | 已确认的组织方式 |
| --- | --- |
| 单项 Skill | 独立维护一项可复用能力或流程，例如页面交互验证、接口契约检查、仓库候选初审；明确适用条件、输入、输出及工具需求。 |
| 角色能力组合 | 为 Manager、Leader、验证负责人或 Worker 选择所需 Skill。例如 Leader 使用仓库拆解与初审能力，验证 Worker 使用页面或接口检查能力。组合不增加角色身份或权限。 |
| 事项工作说明 | 关联本条 Issue 使用的确定 Skill 版本，以及本次目标、业务解释和特殊步骤，由 Manager 统筹落实到具体计划。一次性要求不直接改写长期公共 Skill。 |

事项工作说明与 Plan Version 分开理解：前者说明本次如何使用相关能力及特殊要求；后者承载具体任务、依赖、必检项和上限。两者的实际记录拆分、字段和存储形式尚未冻结。

```text
可复用 Skill 的确定版本
          |
      按角色组合能力
          |
   AgentTeams 分发与运行时加载
          |
   本条 Issue 引用确定版本
   + 事项工作说明
          |
   当前有效计划：任务、依赖、必检项及上限
          |
   执行、复核、证据与 ChangeSet 关联
```

图表示职责和使用关系，不宣称上游已提供按 Issue 的版本隔离。具体加载及核对方法仍需接入验证。

### 沿用的执行边界

- Skill 声明工具需求，RepoMesh 实际核验权限、容量、独立性和任务启动条件；声明不等于授权。
- 缺失业务解释仍按模式处理：YOLO 由 Manager 选择并记录，审批模式由 Manager 请人决定；不覆盖已有明确要求。
- 新 Skill 版本发布不静默改变在途计划或验收要求。影响本轮工作时继续按 ADR-0003 处理，Skill 版本与验证依据关联。
- 同一代码组合采用不同 Skill 等上下文时，按已确认 CS4 新增相应验证记录并重新判断证据，不改写历史结论。
- 平台／项目维护归属与角色使用组合是不同维度。按已同意的 H2、H3，平台提供通用版本，项目明确选用或维护派生版本；Agent 起草、改进和验证，项目管理员或有 Skill 发布权限的人发布项目版本。整个模块仍暂缓开发。

## 支付状态筛选场景

```text
页面验证 Skill v1.3
  准备前提、执行操作、检查断言、保存证据
          |
项目运行说明
  前后端启动方法、mock 数据装载方法、账号可见范围
          |
本条 Issue 的工作说明
  引用 v1.3、采用的支付状态解释、本次检查要求
          |
具体计划
  环境准备任务、独立验证任务、依赖、失败处理与上限
          |
固定组合 F1 + B1、实际环境与数据 -> 检查及证据
```

筛选、清除条件和分页只作为本例已约定的检查范围，不作为所有 Issue 的默认清单。若本条 Issue 明确要求导出也遵循筛选，在本次工作说明和计划中落实导出检查，不自动改变其他 Issue 的公共流程。

执行期间出现页面验证 Skill v1.4，本条 Issue 仍可追溯原先依据的 v1.3。按已同意的 H10，发布先使版本可选，项目另行决定后续 Issue 默认版本；在途升级说明差异及影响，并遵守计划许可、生效和证据规则。实际运行时怎样确保版本一致仍需在模块恢复实施后验证。

## AgentTeams 静态机制与接口依据

补查基线：`eeaab64391ccaec9118e84977f538aefd40720d6`。以下为 2026-09-08 对该提交官方文件的只读核查，未运行脚本或调用真实 Controller／QwenPaw 服务。它们是上游事实，不是新增的 RepoMesh 产品决定。

| 机制 | 锁定源码所示行为 | 接入含义 |
| --- | --- | --- |
| 内容格式 | Skill 目录含 `SKILL.md` 和可选脚本；`name`、`description` 必填，`assign_when` 是可选的自然语言角色匹配提示。[格式说明](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/manager/agent/worker-skills/README.md) | 可复用内容组织形式；匹配提示不替代能力、权限或独立性核验。 |
| 成员分配与远程来源 | `skills` 是名称列表；`remoteSkills` 可指定 Nacos 来源及 Skill 的 `version` 或 `label`，两者不能同时设置。[类型](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/api/v1beta1/types.go)、[分发实现](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/service/deployer.go) | 来源选择与最终实际内容需分别核查；不据此要求 RepoMesh 首期必须部署 Nacos。 |
| REST 缺口 | Worker 创建 handler 忽略 `remoteSkills`，更新 handler 才写入；ManagerSpec 及其创建／更新 handler 未接通该字段。[现有接口调查](../agentteams-survey-2026-09-07/agentteams-api-cli-survey-2026-09-07.md) | DTO 声明或请求成功不能证明配置生效，需按角色与 runtime 验证。 |
| Manager 分发脚本 | 从管理目录将 Skill 镜像到 `agents/{worker}/skills/{skill}`，核对 `SKILL.md`，修改分配时读回 Worker 记录，并通过 Matrix 通知同步；通知不可用时可保留已上传结果。[脚本](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/manager/agent/skills/worker-management/scripts/push-worker-skills.sh) | 文件上传、成员分配与实际加载不是同一事实；脚本的检查不等于完整内容或 Issue 版本验收。 |
| QwenPaw Worker 加载 | Controller 将 Skill 名称投影到成员运行配置；Worker 有同步分配文件、检查缺失并刷新启用的代码，API 客户端调用 `/api/skills/refresh` 和 `/api/skills/batch-enable`。[投影](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/service/runtime_config.go)、[更新](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/qwenpaw/src/qwenpaw_worker/update.py)、[客户端](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/qwenpaw/src/qwenpaw_worker/api.py) | 这是 runtime 接口，不能写成 Controller 的同名 API，也不证明按 Issue 隔离加载。 |
| QwenPaw Manager 加载 | 同步程序按内容摘要检测改变，替换成员工作区中的同名目录，再刷新和启用 Skill。[同步程序](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/manager/scripts/init/qwenpaw_manager_skill_sync.py) | 内容同步面向工作区；独立 session 不自动证明 Skill 文件或启用状态也按 Issue 隔离。 |
| AgentSpec 与插件 | AgentSpec 包可携带提示文件、Skills、MCP 等内容；TeamHarness 插件属于 runtime 协作基础，两类包不同。[运行配置说明](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/docs/design/member-runtime-config-contract.md)、[包应用实现](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/qwenpaw/src/qwenpaw_worker/update.py) | 整体 Agent 模板更新与单项 Skill 更新分别评估；尚未选定 RepoMesh 的打包形式。 |

架构推断：一个长期 Manager／Leader 参与多条独立 Issue 时，直接覆盖其成员工作区中的公共同名 Skill，可能影响其他 Issue 的后续读取。上述同步与接口证据不能证明原生具备“每条 Issue 固定 Skill 版本”的完整保证。因此 H1 的确定版本引用仍需配套实际加载、内容核对和使用范围的实现验证；不能把版本号记录本身当作隔离已完成。

## 占位范围与后续接续

- H1—H15 已同意的完整规则以 [ADR-0009](../adr/0009-skill-engineering-deferred.md)为准，不再将 H2—H15 列为等待采纳的问题。
- Skill 工程整体标记待定／占位，暂不开发；目前只保留 ADR、专题、术语与导航位置，不创建空实现、接口或功能开关。
- 模块恢复时再细化目录和包格式、版本与内容标识、依赖解析、来源选择、成员分配与 runtime 加载核对、同成员多 Issue 使用不同版本的保证。
- 维护与发布职责、评估方向、Issue 变体、升级、失败、第三方来源及历史规则已记录，但具体 schema、状态枚举、页面和技术契约尚未冻结或验证。
- I 当前按 ADR-0006 采用全自动，后续审批主要规则已确认（见 ADR-0003），审批能力后续开发；J1—J3 项目配置与授权变化的主要规则已确认，下一阶段继续 K 的界面交互。Skill 占位不改变 YOLO、计划和验收的既有边界。

## 确认记录

用户先确认 H1 的工程组织方式；随后要求集中列出剩余 Skill 工程问题，在收到 H2—H15 的建议后回复“同意”，并明确要求写入新的 ADR，同时将整个 Skill 模块标记为待定、先不开发、保留位置。现由 ADR-0009 记录全部方向与暂缓状态；此确认不启动实施。
