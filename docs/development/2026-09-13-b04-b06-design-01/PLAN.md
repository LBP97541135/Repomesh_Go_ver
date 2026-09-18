# B04、B05、B06 本轮设计计划

状态：DESIGN_COMPLETE_CANDIDATE_NOT_ADOPTED。2026-09-13。主设计 Astra，目标 effort=xhigh；三个只读调研明确路由 gpt-5.6-sol / high。当前任务授权优先于旧 Prompt 和技能默认实现流程。

- [x] Ground：环境、基线、既有候选及源码调用链核对。内容版本见 baseline.json；调研和关键证据由主设计复核。
- [x] Sketch B04：来源、版本、秘密保存与安全终结，比较实际不同的方案，写唯一专题及声明。
- [x] Sketch B05：单次测试、专用应用、C05/C06，复用 B04 的版本和秘密基础。
- [x] Sketch B06：P9、原子创建、约束、恢复、DB01—08/CB01—05。
- [x] Agree：已交付推荐与待采用选择；用户尚未采用，不把本轮设计授权当产品采用。
- [x] 独立复核及结构检查，闭环本轮问题，更新实际设计待办。[复核PASS](REVIEW.md)，[实际检查](checks.json)。
- Implement：本轮排除。
- Scrap：若证据推翻候选，在文档中修订，不写临时产品实现。

## 文件编辑归属

主设计唯一编辑者。Sol 调研只读，不拥有产品或文档写权限。独立复核者不参与设计附件编写。

| 单元 | 可编辑文件 |
| --- | --- |
| 全轮附件 | 本目录 README.md、PLAN.md、baseline.json、initial-status.txt、source-evidence.json、original-documents.zip、SOURCE-AUDIT.md、DECISIONS.md、B04.md、B05.md、B06.md、b04.go.txt、b05.go.txt、b06.go.txt、frontend.ts.txt、migration-design.md、ACCEPTANCE.md、COMPATIBILITY.md、REVIEW.md、checks.json、verify-design.py、changes.diff、output-manifest.json |
| B04 唯一专题 | docs/current/backend-first-batch-sources-draft.md、backend-model-operations-draft.md、model-settings-browser-api-draft.md 中 B04 增量 |
| B05 唯一专题 | 上述跨批专题中 B05 增量；docs/current/first-batch-browser-api-contract.md 的 C05；model-project-apply-design.md 只回链 C06 |
| B06 唯一专题 | docs/current/backend-first-batch-persistence.md、issue-configuration-binding-design.md、issue-page-create-api-contract.md 中本轮约束／采用状态说明 |
| 最终导航／交接 | docs/current/README.md、HANDOFF.md、DEVELOPMENT-START.md、IMPLEMENTATION-PLAN.md、ASTRA-DESIGN-PREPARATION.md、HANDOFF-BACKEND-DESIGN.md、HANDOFF-PAGE-API-DESIGN.md、NEXT-TASK-B04-PROMPT.md、project-development.md，仅本轮状态／链接及已核实过期描述 |

根 README 使用方式未变，不编辑。旧 reviews、worktree、B02 失败／暂停／恢复记录不编辑。cmd/internal/web/src、迁移、依赖、构建脚本、运行配置不编辑。不运行服务、数据库、浏览器、容器、模型/GitHub请求或历史实验，不读取秘密，不提交或推送。
