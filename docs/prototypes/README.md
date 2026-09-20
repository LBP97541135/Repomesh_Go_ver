# 页面原型导航

更新：2026-09-20。先看[串联预览](index.html)，采用范围和机器契约以[页面交接](../current/HANDOFF-PAGE-API-DESIGN.md)及[首批候选包](../current/first-batch-complete-review.md)为准。原型均为内存演示，不接真实账号、密钥、数据库或运行服务。

## 当前入口

| 页面 | 原型与状态 |
| --- | --- |
| 登录／建项目 | [项目入口原稿](repomesh-project-entry-prototype.html)、[登录恢复](index.html#auth-recovery)。两步流程及列明恢复 UI 已采用，认证协议候选未实现。 |
| 项目设置／选仓 | [项目设置原稿](repomesh-project-settings-prototype.html)、[仓库选择](index.html#repository-picker)。F02／F03 列明 UI 已采用。 |
| 模型配置／测试／应用 | [供应商分栏原稿](repomesh-model-provider-prototype.html)、[测试](index.html#model-test)、[用于项目](index.html#model-apply)、[Key 保存](index.html#key-save)。分栏及 F04 列明 UI 已采用，真实协议候选；Key 安全终结为新增待采用交互。 |
| Issue 创建 | [居中弹窗](repomesh-issue-modal-prototype.html)、[关联会话](repomesh-issue-conversation-prototype.html)、[提交反馈](repomesh-issue-submit-prototype.html)。所列呈现已采用，真实恢复未实现。 |
| Issue 详情 | [首批最小概览](index.html#issue-overview)、[完整详情对照](repomesh-issue-detail-prototype.html)。新增 UI 范围仍待采用。 |
| 会话／DAG／房间 | [会话结构来源](repomesh-conversations-issues.html)、[只读 DAG](repomesh-issue-dag-prototype.html)、[悬浮入口与同页右栏](repomesh-conversation-dock-prototype.html)。列明布局已采用，运行读取与执行协议未完成。 |
| DAG 计划执行进度 | [计划执行进度原稿](dag-plan-progress.html)（2026-09-16 独立单页，不进串联预览）。分层自动布局、48px 白卡加左缘状态色条、可开工虚线框、点选节点浮出详情卡、模拟推进按钮；数据形状对齐 `GET /api/v1/projects/{id}/workflow?includeTasks=true`。前端 `PlanDagPanel` 已按其节点形态、选中高亮与点选详情卡落地；本页仍是内存模拟，真实读取未接线。 |
| 消息目标与澄清 | [消息原稿](repomesh-message-target-prototype.html)。新控件仍待评审，最小本地消息契约不等于真实 Manager 往返。 |

09-12 已修复模型列表与应用预览的测试观察不一致，并补 Key 终结候选的模拟分支。仅同一快照共享测试观察，不修改不可变配置或新增测试通过门槛；验证边界见[修订记录](../reviews/2026-09-12-design-fixes/README.md)。

## 生成与保留范围

运行 `python docs/prototypes/assemble-prototypes.py` 生成 `index.html`。直接输入为项目入口、消息工作区、项目设置、供应商分栏四份 HTML，加首批总览模板；六段 JS 分别嵌入所属家族。修改源片段或模板后重新生成，不手改生成文件。

本目录保留 12 份原稿及其必要源片段；其中[计划执行进度板](dag-plan-progress.html)是独立单页，不进串联预览也不进生成清单。4 份明确被替代的稿已归档，“全貌与原稿”仍可打开历史对照。生成清单核对当前 11 份原稿，历史原始字节及归档映射见[本次归档](../archive/2026-09-12-development-preparation/README.md)。[串联专题](../current/prototype-walkthrough-design.md)维护具体路由和嵌入关系。

| 历史稿 | 替代依据 |
| --- | --- |
| [固定会话导航 r1](../archive/2026-09-12-development-preparation/docs/prototypes/repomesh-conversation-navigation-prototype.html) | 已由悬浮入口／同页右栏 r2 替代。 |
| [模型连接弹窗 r2](../archive/2026-09-12-development-preparation/docs/prototypes/repomesh-model-settings-prototype.html) | 已由供应商分栏 r3 替代。 |
| [项目优先旧稿](../archive/2026-09-12-development-preparation/docs/prototypes/repomesh-project-first.html) | 属会话与 Issue 分离前的历史结构。 |
| [草稿与房间旧稿](../archive/2026-09-12-development-preparation/docs/prototypes/repomesh-rooms-prototype.html) | 聊天整体转正与旧草稿绑定已被替代。 |

整理前的逐次展示、原始本机位置和完整来源记录保留在[旧原型导航](../archive/2026-09-12-development-preparation/docs/prototypes/README.md)。历史稿中的状态、角色和操作指令不恢复为现行规则。
