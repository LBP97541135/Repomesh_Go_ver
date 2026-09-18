---
status: accepted
date: 2026-09-09
---

# ADR-0020：创建 Issue 时调用受控 Python 仓库分析插件

用户在旧项目源码调查后选择：“可以先把仓库关联分析做成在建立issue的时候加一个仓库分析按钮，做成python写的插件”。采用创建表单中的可选仓库分析入口，以第一方 Python 插件复用旧仓库解析和候选分析逻辑；用户应用建议并调整后，按最终选择创建 Issue。历史决策分析及向量检索不作为本期前置能力。

Go 核心拥有身份、权限、固定材料、持久分析作业、结果和 Issue 创建事实；Python 只处理受限输入并返回候选、关系、证据和覆盖说明。分析发生在 Issue 建立之前，不能依赖旧计划快照、审批链或 AgentTeams 运行就绪。采用随产品配套发布、可启停的受控 Python 子进程，通过版本化 JSON 接口调用；启动和资源生命周期由既有受限执行职责管理。

本决定局部扩展 [ADR-0010](0010-technology-stack-and-modular-monolith.md) 的自有后端语言范围与 [ADR-0013](0013-web-coordinator-host-executor-processes.md) 的三类 Go 进程安排：业务与控制逻辑继续在 Go 工程内，额外允许 Python 分析进程。相比全部重写为 Go，优先保留旧算法；代价是维护 Python 依赖、跨进程契约、限额和恢复。没有采用独立微服务体系或通用第三方插件安装平台，[ADR-0014](0014-in-process-graph-plugin.md) 的进程内 Graph 设计及 [ADR-0009](0009-skill-engineering-deferred.md) 的 Skill 暂缓范围保持。

分析属于辅助建议，失败或停用允许手动选仓；它不自动扩仓、产生有效计划或授予执行权。交互、接口和迁移范围统一见[现行专题](../current/issue-creation-repository-analysis.md)，静态来源依据见[旧项目调查](../research/legacy-analysis-plugin-feasibility-2026-09-10.md)。此处确认设计，不表示按钮、接口、插件或运行验收已经完成。
