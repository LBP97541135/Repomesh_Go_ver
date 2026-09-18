# B04—B06 前置设计交付

2026-09-13，状态：DESIGN_COMPLETE / RECOMMENDATIONS_NOT_ADOPTED / IMPLEMENTATION_NOT_STARTED。主设计Astra（任务要求xhigh），三个只读调研明确使用GPT-5.6 Sol/high；独立复核者未参与编写，最终PASS、无开放P0/P1/P2。本轮只改声明过的文档与附件。

| 批次 | 设计交付 | 唯一规则/边界 |
| --- | --- | --- |
| B04 | [设计](B04.md)、[Go声明](b04.go.txt) | 来源§8、模型内部§7、模型HTTP§8；秘密原子保存/终结、固定版本和部署导入 |
| B05 | [设计](B05.md)、[Go声明](b05.go.txt) | 来源§9、模型内部§8、首批HTTP C05、模型HTTP C06；一次测试、只换模型及摘要 |
| B06 | [设计](B06.md)、[Go声明](b06.go.txt) | 持久化§7、配置绑定§8、创建HTTP§11；P9和手动原子创建，不接运行 |

共用附件：[方案比较与待采用决定](DECISIONS.md)、[TS声明](frontend.ts.txt)、[迁移设计](migration-design.md)、[验收映射](ACCEPTANCE.md)、[兼容审查](COMPATIBILITY.md)、[源码核对](SOURCE-AUDIT.md)、[独立复核](REVIEW.md)、[检查结果](checks.json)、[任务增量](changes.diff)、[输出清单](output-manifest.json)。所有业务运行验收均NOT_RUN。

[计划及编辑边界](PLAN.md)区分设计、采用、实现和验收。原始环境/内容版本为[baseline.json](baseline.json)及[initial-status.txt](initial-status.txt)，允许编辑文档的原始字节存于[归档](original-documents.zip)，后续来源绑定在[source-evidence.json](source-evidence.json)。changes.diff与开始任务时工作区字节相比，避免把既有未提交B02/B03算成本轮改动。

推荐采用D01—D08及S06后按各批U04/U05/U06单元实施；模型Key UI/协议、次数预算和参数、C05/C06、P9及S06仍需用户取舍。采用前不能将附件直接当现行产品规范，拒绝其中跨批依赖时须一起修订。

B02外部暂停与历史失败/恢复记录保留；B03本地集成验收状态不变；B04/B05/B06实现状态不因设计交付变更。B09只完成本轮必要数据兼容静态审查，完整G1/G2未完成。完成本轮设计后停止，不提交、不推送、不开始B07—B11。
